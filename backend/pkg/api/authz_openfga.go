package api

import (
	"backend/pkg/config"
	backendsecurity "backend/pkg/security"
	"backend/pkg/utils"
)

// newAuthzOpenFGA wires the OpenFGA-backed authorizer used in shadow/enforce
// mode. The implementation stays in openfga_shadow.go so the public entrypoint
// remains stable while the authorizer logic stays isolated.
func newAuthzOpenFGA(cfg *config.APIConfig, logger *utils.Logger) backendsecurity.Authorizer {
	return newConfiguredAuthorizer(cfg, logger)
}
