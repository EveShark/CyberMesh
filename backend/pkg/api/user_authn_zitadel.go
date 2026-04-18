package api

import "backend/pkg/config"

// newUserAuthnZitadel wires the hosted ZITADEL JWT validator used by the
// backend auth middleware. The implementation stays in zitadel_jwt.go so the
// public entrypoint remains stable while the validator logic stays isolated.
func newUserAuthnZitadel(cfg *config.APIConfig) (bearerTokenValidator, error) {
	return newZitadelJWTValidator(cfg)
}
