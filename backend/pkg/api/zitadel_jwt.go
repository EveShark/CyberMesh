package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"backend/pkg/config"
	"backend/pkg/security/contracts"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

type validatedBearerIdentity struct {
	PrincipalID   string
	PrincipalType contracts.PrincipalType
	Subject       string
	Username      string
	Email         string
}

type bearerTokenValidator interface {
	ValidateToken(context.Context, string) (*validatedBearerIdentity, error)
}

type zitadelJWTValidator struct {
	issuer   string
	audience string
	keyfunc  jwt.Keyfunc
	cancel   context.CancelFunc
}

type zitadelClaims struct {
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Name              string `json:"name"`
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
	refreshInterval := cfg.ZitadelJWKSRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = time.Hour
	}
	httpTimeout := cfg.ZitadelJWKSTimeout
	if httpTimeout <= 0 {
		httpTimeout = 5 * time.Second
	}
	jwksCtx, cancel := context.WithCancel(context.Background())
	jwks, err := keyfunc.NewDefaultOverrideCtx(jwksCtx, []string{jwksURL}, keyfunc.Override{
		Client:          &http.Client{Timeout: httpTimeout},
		HTTPTimeout:     httpTimeout,
		RefreshInterval: refreshInterval,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create jwks cache: %w", err)
	}

	return &zitadelJWTValidator{
		issuer:   issuer,
		audience: audience,
		keyfunc:  jwks.Keyfunc,
		cancel:   cancel,
	}, nil
}

func (v *zitadelJWTValidator) Close() error {
	if v != nil && v.cancel != nil {
		v.cancel()
	}
	return nil
}

func (v *zitadelJWTValidator) ValidateToken(_ context.Context, token string) (*validatedBearerIdentity, error) {
	claims := &zitadelClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, v.keyfunc, jwt.WithValidMethods([]string{
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

	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return nil, errors.New("token subject is empty")
	}
	username := strings.TrimSpace(claims.PreferredUsername)
	if username == "" {
		username = strings.TrimSpace(claims.Name)
	}

	return &validatedBearerIdentity{
		PrincipalID:   "user:" + subject,
		PrincipalType: contracts.PrincipalTypeUser,
		Subject:       subject,
		Username:      username,
		Email:         strings.TrimSpace(claims.Email),
	}, nil
}
