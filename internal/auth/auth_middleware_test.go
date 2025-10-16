package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAPIKeyMiddleware_StaticKey(t *testing.T) {
	middleware := APIKeyMiddleware(APIKeyMiddlewareConfig{
		StaticKeys: []string{"static"},
		Logger:     discardLogger,
	})

	var called bool
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(apiKeyHeader, "static")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAPIKeyMiddleware_TemporaryKeyAuthorized(t *testing.T) {
	repo := newMemoryRepository()
	service := NewKeyService(repo, discardLogger, nil, ServiceConfig{})
	resp, err := service.IssueTemporaryKey(httptest.NewRequest("", "/", nil).Context(), IssueRequest{
		Label:      "demo",
		UsageLimit: 1,
		TTL:        time.Hour,
		Operator:   "operator",
	})
	if err != nil {
		t.Fatalf("issue key: %v", err)
	}

	middleware := APIKeyMiddleware(APIKeyMiddlewareConfig{
		KeyService:     service,
		Logger:         discardLogger,
		FeatureEnabled: true,
	})

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/convert", nil)
	req.Header.Set(apiKeyHeader, resp.Key)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	record, _, err := service.ValidateAndConsume(req.Context(), resp.Key)
	if err == nil {
		t.Fatalf("expected usage to be exhausted after second consume, got remaining %d", record.RemainingUsage)
	}
}

func TestAPIKeyMiddleware_TemporaryKeyUnauthorized(t *testing.T) {
	repo := newMemoryRepository()
	service := NewKeyService(repo, discardLogger, nil, ServiceConfig{})

	middleware := APIKeyMiddleware(APIKeyMiddlewareConfig{
		KeyService:     service,
		Logger:         discardLogger,
		FeatureEnabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(apiKeyHeader, "missing-key")
	rec := httptest.NewRecorder()

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAPIKeyMiddleware_FirestoreError(t *testing.T) {
	service := NewKeyService(&failingRepository{}, discardLogger, nil, ServiceConfig{})
	middleware := APIKeyMiddleware(APIKeyMiddlewareConfig{
		KeyService:     service,
		Logger:         discardLogger,
		FeatureEnabled: true,
		RetryAfter:     3 * time.Second,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(apiKeyHeader, "dynamic")
	rec := httptest.NewRecorder()

	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler should not be called")
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "3" {
		t.Fatalf("expected retry-after 3, got %s", rec.Header().Get("Retry-After"))
	}
}

type failingRepository struct{}

func (f *failingRepository) CreateTemporaryKey(ctx context.Context, key APIKey) error {
	return errors.New("not implemented")
}

func (f *failingRepository) Get(ctx context.Context, key string) (APIKey, error) {
	return APIKey{}, errors.New("not implemented")
}

func (f *failingRepository) Consume(ctx context.Context, key string, now time.Time) (APIKey, error) {
	return APIKey{}, errors.New("boom")
}

func (f *failingRepository) Revoke(ctx context.Context, key string, now time.Time) (APIKey, error) {
	return APIKey{}, errors.New("not implemented")
}

func (f *failingRepository) Delete(ctx context.Context, key string) error {
	return nil
}

func (f *failingRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	return 0, nil
}

func (f *failingRepository) CountActive(ctx context.Context, now time.Time) (int, error) {
	return 0, nil
}
