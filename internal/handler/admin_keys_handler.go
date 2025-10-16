package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pdf2jpg/internal/auth"
)

const (
	defaultUsageLimit = 10
	defaultTTLMinutes = 10080
	minUsageLimit     = 1
	maxUsageLimit     = 1000
	minTTLMinutes     = 15
	maxTTLMinutes     = 10080
)

// KeyAdminHandler exposes admin operations for temporary API keys.
type KeyAdminHandler struct {
	service KeyManagementService
	logger  *log.Logger
}

type KeyManagementService interface {
	IssueTemporaryKey(ctx context.Context, req auth.IssueRequest) (auth.IssueResponse, error)
	Get(ctx context.Context, key string) (auth.APIKey, error)
	Revoke(ctx context.Context, key string, operator string) (auth.APIKey, error)
	CleanupExpired(ctx context.Context, limit int) (int, error)
}

func NewKeyAdminHandler(service KeyManagementService, logger *log.Logger) *KeyAdminHandler {
	return &KeyAdminHandler{
		service: service,
		logger:  logger,
	}
}

func (h *KeyAdminHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/admin/api-keys", h.issueKey)
	mux.HandleFunc("/admin/api-keys/", h.routeKeyActions)
	mux.HandleFunc("/admin/api-keys/cleanup", h.cleanup)
}

func (h *KeyAdminHandler) routeKeyActions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	const base = "/admin/api-keys/"
	if len(path) <= len(base) {
		writeAdminError(w, http.StatusNotFound, "not found")
		return
	}
	rest := path[len(base):]
	switch {
	case r.Method == http.MethodGet:
		h.getKey(w, r, rest)
	case r.Method == http.MethodPost && strings.HasSuffix(rest, "/revoke"):
		key := strings.TrimSuffix(rest, "/revoke")
		h.revokeKey(w, r, key)
	default:
		writeAdminError(w, http.StatusNotFound, "not found")
	}
}

func (h *KeyAdminHandler) issueKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Label      string `json:"label"`
		UsageLimit *int   `json:"usageLimit"`
		TTLMinutes *int   `json:"ttlMinutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeAdminError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	usage := defaultUsageLimit
	if req.UsageLimit != nil {
		usage = *req.UsageLimit
	}
	if usage < minUsageLimit || usage > maxUsageLimit {
		writeAdminError(w, http.StatusBadRequest, "usageLimit out of range")
		return
	}

	ttl := defaultTTLMinutes
	if req.TTLMinutes != nil {
		ttl = *req.TTLMinutes
	}
	if ttl < minTTLMinutes || ttl > maxTTLMinutes {
		writeAdminError(w, http.StatusBadRequest, "ttlMinutes out of range")
		return
	}

	operator := auth.AdminOperatorFromContext(r.Context())
	resp, err := h.service.IssueTemporaryKey(r.Context(), auth.IssueRequest{
		Label:      req.Label,
		UsageLimit: usage,
		TTL:        time.Duration(ttl) * time.Minute,
		Operator:   operator,
	})
	if err != nil {
		h.logger.Printf("ERROR: issue temporary key: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "failed to issue key")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"key":            resp.Key,
		"label":          resp.Record.Label,
		"createdAt":      resp.Record.CreatedAt.UTC().Format(time.RFC3339),
		"expiresAt":      resp.Record.ExpiresAt.UTC().Format(time.RFC3339),
		"maxUsage":       resp.Record.MaxUsage,
		"remainingUsage": resp.Record.RemainingUsage,
		"status":         resp.Record.Status(time.Now().UTC()),
	})
}

func (h *KeyAdminHandler) getKey(w http.ResponseWriter, r *http.Request, key string) {
	record, err := h.service.Get(r.Context(), key)
	if errors.Is(err, auth.ErrKeyNotFound) {
		writeAdminError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.logger.Printf("ERROR: fetch key status: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "failed to fetch key")
		return
	}

	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"label":          record.Label,
		"createdAt":      record.CreatedAt.UTC().Format(time.RFC3339),
		"expiresAt":      record.ExpiresAt.UTC().Format(time.RFC3339),
		"maxUsage":       record.MaxUsage,
		"remainingUsage": record.RemainingUsage,
		"status":         record.Status(now),
		"revokedAt":      formatOptionalTime(record.RevokedAt),
	})
}

func (h *KeyAdminHandler) revokeKey(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodPost {
		writeAdminError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	record, err := h.service.Revoke(r.Context(), key, auth.AdminOperatorFromContext(r.Context()))
	if errors.Is(err, auth.ErrKeyNotFound) {
		writeAdminError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		h.logger.Printf("ERROR: revoke key: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "failed to revoke key")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"label":          record.Label,
		"revokedAt":      formatOptionalTime(record.RevokedAt),
		"remainingUsage": record.RemainingUsage,
		"status":         record.Status(time.Now().UTC()),
	})
}

func (h *KeyAdminHandler) cleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAdminError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	limit := auth.DefaultCleanupLimit()
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			if parsed > auth.DefaultCleanupLimit() {
				parsed = auth.DefaultCleanupLimit()
			}
			limit = parsed
		}
	}

	count, err := h.service.CleanupExpired(r.Context(), limit)
	if err != nil {
		h.logger.Printf("ERROR: cleanup expired keys: %v", err)
		writeAdminError(w, http.StatusInternalServerError, "cleanup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": count})
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	val := t.UTC().Format(time.RFC3339)
	return &val
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeAdminError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
