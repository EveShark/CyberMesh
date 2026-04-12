package api

import "backend/pkg/config"

// newUserAuthnZitadel wires the hosted ZITADEL JWT validator used by the
// backend auth middleware. The implementation stays in zitadel_jwt.go; this
// file exists to satisfy the Phase 2 deliverable boundary explicitly.
func newUserAuthnZitadel(cfg *config.APIConfig) (bearerTokenValidator, error) {
	return newZitadelJWTValidator(cfg)
}
