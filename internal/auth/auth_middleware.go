package auth

import (
	"encoding/json"
	"log"
	"net/http"
)

const apiKeyHeader = "X-API-Key"

// APIKeyMiddleware validates the X-API-Key header against the provided list and logs unauthorized attempts.
func APIKeyMiddleware(validKeys []string, logger *log.Logger) func(http.Handler) http.Handler {
	keySet := make(map[string]struct{}, len(validKeys))
	for _, key := range validKeys {
		keySet[key] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get(apiKeyHeader)
			if _, ok := keySet[apiKey]; !ok {
				logger.Printf("WARN: unauthorized request method=%s path=%s", r.Method, r.URL.Path)
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
