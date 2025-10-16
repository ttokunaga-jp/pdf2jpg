package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pdf2jpg/internal/auth"
)

var discardLogger = log.New(io.Discard, "", 0)

func TestKeyAdminHandler_IssueSuccess(t *testing.T) {
	service := &stubKeyService{
		issueResponse: auth.IssueResponse{
			Key: "TEMPKEY",
			Record: auth.APIKey{
				Label:          "trial",
				CreatedAt:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				ExpiresAt:      time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC),
				MaxUsage:       5,
				RemainingUsage: 5,
			},
		},
	}
	handler := newAdminTestServer(service)

	body := bytes.NewBufferString(`{"label":"trial","usageLimit":5,"ttlMinutes":60}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys", body)
	req.Header.Set("X-Admin-Key", "master")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["key"] != "TEMPKEY" {
		t.Fatalf("expected key TEMPKEY, got %#v", resp["key"])
	}
	if !service.issueCalled {
		t.Fatal("expected issue to be invoked")
	}
	if service.issueRequest.UsageLimit != 5 {
		t.Fatalf("expected usage limit 5, got %d", service.issueRequest.UsageLimit)
	}
}

func TestKeyAdminHandler_IssueValidationError(t *testing.T) {
	service := &stubKeyService{}
	handler := newAdminTestServer(service)

	body := bytes.NewBufferString(`{"usageLimit":0}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys", body)
	req.Header.Set("X-Admin-Key", "master")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if service.issueCalled {
		t.Fatal("issue should not be called on validation failure")
	}
}

func TestKeyAdminHandler_GetNotFound(t *testing.T) {
	service := &stubKeyService{
		getErr: auth.ErrKeyNotFound,
	}
	handler := newAdminTestServer(service)

	req := httptest.NewRequest(http.MethodGet, "/admin/api-keys/TEMP", nil)
	req.Header.Set("X-Admin-Key", "master")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestKeyAdminHandler_Cleanup(t *testing.T) {
	service := &stubKeyService{
		cleanupCount: 3,
	}
	handler := newAdminTestServer(service)

	req := httptest.NewRequest(http.MethodPost, "/admin/api-keys/cleanup?limit=500", nil)
	req.Header.Set("X-Admin-Key", "master")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if resp["deleted"] != 3 {
		t.Fatalf("expected deleted=3, got %d", resp["deleted"])
	}
	if service.cleanupLimit != auth.DefaultCleanupLimit() {
		t.Fatalf("expected limit to be capped at default, got %d", service.cleanupLimit)
	}
}

func newAdminTestServer(service KeyManagementService) http.Handler {
	mux := http.NewServeMux()
	NewKeyAdminHandler(service, discardLogger).Register(mux)
	return auth.AdminAuthMiddleware(auth.AdminMiddlewareConfig{
		MasterKeys: []string{"master"},
		Logger:     discardLogger,
	})(mux)
}

type stubKeyService struct {
	issueCalled   bool
	issueRequest  auth.IssueRequest
	issueResponse auth.IssueResponse
	issueErr      error

	getResponse auth.APIKey
	getErr      error

	revokeResponse auth.APIKey
	revokeErr      error

	cleanupCount int
	cleanupErr   error
	cleanupLimit int
}

func (s *stubKeyService) IssueTemporaryKey(ctx context.Context, req auth.IssueRequest) (auth.IssueResponse, error) {
	s.issueCalled = true
	s.issueRequest = req
	return s.issueResponse, s.issueErr
}

func (s *stubKeyService) Get(ctx context.Context, key string) (auth.APIKey, error) {
	return s.getResponse, s.getErr
}

func (s *stubKeyService) Revoke(ctx context.Context, key string, operator string) (auth.APIKey, error) {
	return s.revokeResponse, s.revokeErr
}

func (s *stubKeyService) CleanupExpired(ctx context.Context, limit int) (int, error) {
	s.cleanupLimit = limit
	return s.cleanupCount, s.cleanupErr
}
