package auth

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

const (
	apiKeyHeader         = "X-API-Key"
	adminKeyHeader       = "X-Admin-Key"
	defaultRetryAfterSec = 5
)

// APIKeyMiddlewareConfig configures the behaviour of the API key middleware.
type APIKeyMiddlewareConfig struct {
	StaticKeys     []string
	KeyService     *KeyService
	Logger         *log.Logger
	FeatureEnabled bool
	RetryAfter     time.Duration
}

// APIKeyMiddleware validates the X-API-Key header against both static and Firestore backed keys.
func APIKeyMiddleware(cfg APIKeyMiddlewareConfig) func(http.Handler) http.Handler {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	staticSet := make(map[string]struct{}, len(cfg.StaticKeys))
	for _, key := range cfg.StaticKeys {
		staticSet[key] = struct{}{}
	}
	retryAfter := cfg.RetryAfter
	if retryAfter == 0 {
		retryAfter = defaultRetryAfterSec * time.Second
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get(apiKeyHeader)
			if apiKey == "" {
				logger.Printf("WARN: missing api key method=%s path=%s", r.Method, r.URL.Path)
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			if _, ok := staticSet[apiKey]; ok {
				next.ServeHTTP(w, r.WithContext(withAPIKey(r.Context(), apiKey)))
				return
			}

			if !cfg.FeatureEnabled || cfg.KeyService == nil {
				logger.Printf("WARN: unknown api key method=%s path=%s", r.Method, r.URL.Path)
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			record, outcome, err := cfg.KeyService.ValidateAndConsume(r.Context(), apiKey)
			switch outcome {
			case validationOutcomeAuthorized:
				ctx := withAPIKey(r.Context(), apiKey)
				ctx = withTemporaryKey(ctx, record)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			case validationOutcomeError:
				logger.Printf("ERROR: firestore validation failure api_key_hash=%s err=%v", hashIdentifier(apiKey, apiKeyHashPrefixLength), err)
				w.Header().Set("Retry-After", formatRetryAfter(retryAfter))
				writeJSONError(w, outcome.httpStatus(), outcome.errorMessage())
			default:
				logger.Printf("WARN: inactive api key outcome=%s api_key_hash=%s", outcome, hashIdentifier(apiKey, apiKeyHashPrefixLength))
				writeJSONError(w, outcome.httpStatus(), outcome.errorMessage())
			}
		})
	}
}

type apiKeyContextKey string

const (
	ctxAPIKey          apiKeyContextKey = "api_key"
	ctxTemporaryRecord apiKeyContextKey = "temporary_key"
	ctxAdminOperator   apiKeyContextKey = "admin_operator"
)

func withAPIKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, ctxAPIKey, key)
}

func withTemporaryKey(ctx context.Context, record APIKey) context.Context {
	return context.WithValue(ctx, ctxTemporaryRecord, record)
}

func withAdminOperator(ctx context.Context, operator string) context.Context {
	return context.WithValue(ctx, ctxAdminOperator, operator)
}

// AdminOperatorFromContext returns the raw admin key for auditing.
func AdminOperatorFromContext(ctx context.Context) string {
	if v := ctx.Value(ctxAdminOperator); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func formatRetryAfter(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// APIKeyFromContext returns the validated API key value when available.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	if v := ctx.Value(ctxAPIKey); v != nil {
		if s, ok := v.(string); ok {
			return s, true
		}
	}
	return "", false
}

// TemporaryKeyFromContext returns the temporary key record when the current request used one.
func TemporaryKeyFromContext(ctx context.Context) (APIKey, bool) {
	if v := ctx.Value(ctxTemporaryRecord); v != nil {
		if rec, ok := v.(APIKey); ok {
			return rec, true
		}
	}
	return APIKey{}, false
}
