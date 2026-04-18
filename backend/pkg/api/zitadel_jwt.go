package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"backend/pkg/config"
	"backend/pkg/security/contracts"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

type validatedBearerIdentity struct {
	PrincipalID   string
	PrincipalType contracts.PrincipalType
	Subject       string
	Username      string
	Email         string
	ClientID      string
}

type bearerTokenValidator interface {
	ValidateToken(context.Context, string) (*validatedBearerIdentity, error)
}

type zitadelJWTValidator struct {
	issuer          string
	audience        string
	jwksURL         string
	jwksJSON        string
	httpTimeout     time.Duration
	refreshInterval time.Duration

	mu      sync.RWMutex
	keyfunc jwt.Keyfunc
	cancel  context.CancelFunc
}

type zitadelClaims struct {
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	ClientID          string `json:"client_id"`
	AuthorizedParty   string `json:"azp"`
	jwt.RegisteredClaims
}

func newZitadelJWTValidator(cfg *config.APIConfig) (bearerTokenValidator, error) {
	issuer := strings.TrimSpace(cfg.ZitadelIssuer)
	clientID := strings.TrimSpace(cfg.ZitadelClientID)
	audience := strings.TrimSpace(cfg.ZitadelAudience)
	if issuer == "" || clientID == "" {
		return nil, nil
	}
	if audience == "" {
		audience = clientID
	}

	jwksURL := strings.TrimRight(issuer, "/") + "/oauth/v2/keys"
	jwksJSON := strings.TrimSpace(cfg.ZitadelJWKSJSON)
	refreshInterval := cfg.ZitadelJWKSRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = time.Hour
	}
	httpTimeout := cfg.ZitadelJWKSTimeout
	if httpTimeout <= 0 {
		httpTimeout = 5 * time.Second
	}
	validator := &zitadelJWTValidator{
		issuer:          issuer,
		audience:        audience,
		jwksURL:         jwksURL,
		jwksJSON:        jwksJSON,
		httpTimeout:     httpTimeout,
		refreshInterval: refreshInterval,
	}
	if err := validator.reloadJWKS(); err != nil {
		return nil, fmt.Errorf("create jwks cache: %w", err)
	}
	return validator, nil
}

func (v *zitadelJWTValidator) Close() error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cancel != nil {
		v.cancel()
		v.cancel = nil
	}
	return nil
}

func (v *zitadelJWTValidator) ValidateToken(_ context.Context, token string) (*validatedBearerIdentity, error) {
	claims, err := v.parseClaims(token)
	if err != nil && isMissingJWKError(err) {
		if reloadErr := v.reloadJWKS(); reloadErr == nil {
			claims, err = v.parseClaims(token)
		}
	}
	if err != nil {
		return nil, err
	}
	return buildValidatedBearerIdentity(claims)
}

func (v *zitadelJWTValidator) parseClaims(token string) (*zitadelClaims, error) {
	claims := &zitadelClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, v.currentKeyfunc(), jwt.WithValidMethods([]string{
		jwt.SigningMethodRS256.Alg(),
		jwt.SigningMethodRS384.Alg(),
		jwt.SigningMethodRS512.Alg(),
		jwt.SigningMethodEdDSA.Alg(),
		jwt.SigningMethodES256.Alg(),
		jwt.SigningMethodES384.Alg(),
		jwt.SigningMethodES512.Alg(),
	}), jwt.WithAudience(v.audience), jwt.WithIssuer(v.issuer), jwt.WithLeeway(30*time.Second), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("token is invalid")
	}
	return claims, nil
}

func buildValidatedBearerIdentity(claims *zitadelClaims) (*validatedBearerIdentity, error) {
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return nil, errors.New("token subject is empty")
	}
	username := strings.TrimSpace(claims.PreferredUsername)
	if username == "" {
		username = strings.TrimSpace(claims.Name)
	}

	clientID := strings.TrimSpace(claims.ClientID)
	if clientID == "" {
		clientID = strings.TrimSpace(claims.AuthorizedParty)
	}

	return &validatedBearerIdentity{
		PrincipalID:   "user:" + subject,
		PrincipalType: contracts.PrincipalTypeUser,
		Subject:       subject,
		Username:      username,
		Email:         strings.TrimSpace(claims.Email),
		ClientID:      clientID,
	}, nil
}

func (v *zitadelJWTValidator) currentKeyfunc() jwt.Keyfunc {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.keyfunc
}

func (v *zitadelJWTValidator) reloadJWKS() error {
	if v.jwksJSON != "" {
		jwks, err := keyfunc.NewJWKSetJSON(json.RawMessage(v.jwksJSON))
		if err != nil {
			return err
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.cancel != nil {
			v.cancel()
			v.cancel = nil
		}
		v.keyfunc = jwks.Keyfunc
		return nil
	}

	jwksCtx, cancel := context.WithCancel(context.Background())
	jwks, err := keyfunc.NewDefaultOverrideCtx(jwksCtx, []string{v.jwksURL}, keyfunc.Override{
		Client:            &http.Client{Timeout: v.httpTimeout},
		HTTPTimeout:       v.httpTimeout,
		RefreshInterval:   v.refreshInterval,
		RefreshUnknownKID: rate.NewLimiter(rate.Every(2*time.Second), 1),
	})
	if err != nil {
		cancel()
		return err
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cancel != nil {
		v.cancel()
	}
	v.keyfunc = jwks.Keyfunc
	v.cancel = cancel
	return nil
}

func isMissingJWKError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "key not found") || strings.Contains(msg, "could not read jwk from storage")
}
