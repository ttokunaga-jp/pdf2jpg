package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestAdminAuthMiddleware_Success(t *testing.T) {
	middleware := AdminAuthMiddleware(AdminMiddlewareConfig{
		MasterKeys: []string{"secret"},
		Logger:     discardLogger,
		RateLimit:  rate.Inf,
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := AdminOperatorFromContext(r.Context()); got != "secret" {
			t.Fatalf("expected operator in context, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/api-keys", nil)
	req.Header.Set(adminKeyHeader, "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAdminAuthMiddleware_MissingKey(t *testing.T) {
	handler := AdminAuthMiddleware(AdminMiddlewareConfig{
		MasterKeys: []string{"secret"},
		Logger:     discardLogger,
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be invoked")
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/api-keys", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAdminAuthMiddleware_RateLimit(t *testing.T) {
	handler := AdminAuthMiddleware(AdminMiddlewareConfig{
		MasterKeys: []string{"secret"},
		Logger:     discardLogger,
		RateLimit:  rate.Every(time.Hour),
		Burst:      1,
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/admin/api-keys", nil)
	req.Header.Set(adminKeyHeader, "secret")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec2.Code)
	}
}
