package api

import (
	"encoding/json"
	"net/http"
	"os"
)

// FrontendConfig represents configuration exposed to frontend
type FrontendConfig struct {
	SupabaseURL       string `json:"supabaseUrl"`
	SupabaseProjectID string `json:"supabaseProjectId"`
	SupabaseKey       string `json:"supabaseKey"`
	DemoMode          string `json:"demoMode"`
}

// handleFrontendConfig returns runtime configuration for frontend
// GET /api/v1/frontend-config
func (s *Server) handleFrontendConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorResponse(w, r, "METHOD_NOT_ALLOWED", "only GET method allowed", http.StatusMethodNotAllowed)
		return
	}

	config := FrontendConfig{
		SupabaseURL:       getEnvOrDefault("VITE_SUPABASE_URL", "https://wcgddjipyslnjstabqaq.supabase.co"),
		SupabaseProjectID: getEnvOrDefault("VITE_SUPABASE_PROJECT_ID", "wcgddjipyslnjstabqaq"),
		SupabaseKey:       getEnvOrDefault("VITE_SUPABASE_PUBLISHABLE_KEY", ""),
		DemoMode:          getEnvOrDefault("VITE_DEMO_MODE", "false"),
	}

	// Set CORS headers for frontend access
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Handle OPTIONS preflight
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := json.NewEncoder(w).Encode(config); err != nil {
		writeErrorResponse(w, r, "INTERNAL_ERROR", "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}
