package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pdf2jpg/internal/auth"
	"pdf2jpg/internal/handler"
	"pdf2jpg/internal/service"
)

const (
	testAPIKey       = "test-key"
	maxUploadBytes   = 10 * 1024 * 1024
	defaultJPEGQual  = 85
	multipartField   = "file"
	expectedFileName = "sample.pdf"
)

type fakeDocument struct {
	pages  int
	img    image.Image
	imgErr error
}

func (f *fakeDocument) NumPage() int {
	return f.pages
}

func (f *fakeDocument) Image(int) (image.Image, error) {
	if f.imgErr != nil {
		return nil, f.imgErr
	}
	return f.img, nil
}

func (f *fakeDocument) Close() error { return nil }

func newTestHandler(t *testing.T, opener func(string) (service.Document, error), keyService *auth.KeyService, enableDynamic bool) http.Handler {
	t.Helper()

	logger := log.New(io.Discard, "", 0)
	restore := service.SetDocumentOpenerForTest(opener)
	t.Cleanup(restore)

	pdfService := service.NewPDFService(defaultJPEGQual)
	convertHandler := handler.NewConvertHandler(pdfService, logger, maxUploadBytes)

	return auth.APIKeyMiddleware(auth.APIKeyMiddlewareConfig{
		StaticKeys:     []string{testAPIKey},
		KeyService:     keyService,
		Logger:         logger,
		FeatureEnabled: enableDynamic,
	})(convertHandler)
}

func TestConvertEndpoint(t *testing.T) {
	successOpener := func(string) (service.Document, error) {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})
		return &fakeDocument{pages: 1, img: img}, nil
	}

	t.Run("success", func(t *testing.T) {
		handler := newTestHandler(t, successOpener, nil, false)
		body, contentType := createMultipartBody(t, expectedFileName, minimalPDF())
		rec := sendConvertRequest(t, handler, body, contentType, testAPIKey)
		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", res.StatusCode)
		}
		if res.Header.Get("Content-Type") != "image/jpeg" {
			t.Fatalf("expected image/jpeg, got %s", res.Header.Get("Content-Type"))
		}
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
			t.Fatalf("expected jpeg header, got %x", data[:2])
		}
	})

	t.Run("missing file", func(t *testing.T) {
		handler := newTestHandler(t, successOpener, nil, false)
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		if err := writer.Close(); err != nil {
			t.Fatalf("close writer: %v", err)
		}
		rec := sendConvertRequest(t, handler, body, writer.FormDataContentType(), testAPIKey)
		assertJSONError(t, rec, http.StatusBadRequest, "file field is required")
	})

	t.Run("invalid extension", func(t *testing.T) {
		handler := newTestHandler(t, successOpener, nil, false)
		body, contentType := createMultipartBody(t, "not-pdf.txt", minimalPDF())
		rec := sendConvertRequest(t, handler, body, contentType, testAPIKey)
		assertJSONError(t, rec, http.StatusBadRequest, "file must be a pdf")
	})

	t.Run("unauthorized", func(t *testing.T) {
		handler := newTestHandler(t, successOpener, nil, false)
		body, contentType := createMultipartBody(t, expectedFileName, minimalPDF())
		rec := sendConvertRequest(t, handler, body, contentType, "bad-key")
		assertJSONError(t, rec, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("file too large", func(t *testing.T) {
		handler := newTestHandler(t, successOpener, nil, false)
		overSized := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("A"), maxUploadBytes+1)...)
		body, contentType := createMultipartBody(t, expectedFileName, overSized)
		rec := sendConvertRequest(t, handler, body, contentType, testAPIKey)
		assertJSONError(t, rec, http.StatusRequestEntityTooLarge, "file too large")
	})

	t.Run("conversion failure", func(t *testing.T) {
		handler := newTestHandler(t, func(string) (service.Document, error) {
			return &fakeDocument{pages: 1, imgErr: assertError("render failed")}, nil
		}, nil, false)
		body, contentType := createMultipartBody(t, expectedFileName, minimalPDF())
		rec := sendConvertRequest(t, handler, body, contentType, testAPIKey)
		assertJSONError(t, rec, http.StatusInternalServerError, "failed to convert pdf")
	})

	t.Run("temporary key usage limit", func(t *testing.T) {
		repo := newTestRepository()
		logger := log.New(io.Discard, "", 0)
		service := auth.NewKeyService(repo, logger, nil, auth.ServiceConfig{})
		resp, err := service.IssueTemporaryKey(context.Background(), auth.IssueRequest{
			Label:      "trial",
			UsageLimit: 1,
			TTL:        time.Hour,
			Operator:   "tester",
		})
		if err != nil {
			t.Fatalf("issue temporary key: %v", err)
		}

		handler := newTestHandler(t, successOpener, service, true)
		body, contentType := createMultipartBody(t, expectedFileName, minimalPDF())
		rec := sendConvertRequest(t, handler, body, contentType, resp.Key)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		body2, contentType2 := createMultipartBody(t, expectedFileName, minimalPDF())
		rec2 := sendConvertRequest(t, handler, body2, contentType2, resp.Key)
		assertJSONError(t, rec2, http.StatusTooManyRequests, "usage limit reached")
	})
}

func createMultipartBody(t *testing.T, filename string, fileBytes []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(multipartField, filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

func sendConvertRequest(t *testing.T, handler http.Handler, body *bytes.Buffer, contentType, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/convert", body)
	req.Header.Set("Content-Type", contentType)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, expectedStatus int, expectedMsg string) {
	t.Helper()
	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != expectedStatus {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected status %d, got %d: %s", expectedStatus, res.StatusCode, string(body))
	}
	if !strings.Contains(res.Header.Get("Content-Type"), "application/json") {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("expected JSON response, got headers=%s body=%s", res.Header, string(body))
	}

	var payload map[string]string
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload["error"] != expectedMsg {
		t.Fatalf("expected error %q, got %q", expectedMsg, payload["error"])
	}
}

func minimalPDF() []byte {
	return []byte("%PDF-1.4\n1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj\n2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj\n3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>endobj\n4 0 obj<< /Length 44 >>stream\nBT /F1 24 Tf 72 120 Td (Hello, PDF) Tj ET\nendstream\nendobj\n5 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>endobj\nxref\n0 6\n0000000000 65535 f \n0000000010 00000 n \n0000000060 00000 n \n0000000115 00000 n \n0000000220 00000 n \n0000000312 00000 n \ntrailer<< /Size 6 /Root 1 0 R >>\nstartxref\n380\n%%EOF\n")
}

type assertError string

func (e assertError) Error() string { return string(e) }

type testRepository struct {
	mu   sync.Mutex
	data map[string]auth.APIKey
}

func newTestRepository() *testRepository {
	return &testRepository{data: make(map[string]auth.APIKey)}
}

func (r *testRepository) CreateTemporaryKey(ctx context.Context, key auth.APIKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[key.Key]; exists {
		return errors.New("duplicate")
	}
	r.data[key.Key] = key
	return nil
}

func (r *testRepository) Get(ctx context.Context, key string) (auth.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.data[key]
	if !ok {
		return auth.APIKey{}, auth.ErrKeyNotFound
	}
	return value, nil
}

func (r *testRepository) Consume(ctx context.Context, key string, now time.Time) (auth.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.data[key]
	if !ok {
		return auth.APIKey{}, auth.ErrKeyNotFound
	}
	switch {
	case value.RevokedAt != nil:
		return auth.APIKey{}, auth.ErrKeyRevoked
	case value.IsExpired(now):
		return auth.APIKey{}, auth.ErrKeyExpired
	case value.RemainingUsage <= 0:
		return auth.APIKey{}, auth.ErrKeyExhausted
	}
	value.RemainingUsage--
	r.data[key] = value
	return value, nil
}

func (r *testRepository) Revoke(ctx context.Context, key string, now time.Time) (auth.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.data[key]
	if !ok {
		return auth.APIKey{}, auth.ErrKeyNotFound
	}
	value.RemainingUsage = 0
	value.RevokedAt = &now
	r.data[key] = value
	return value, nil
}

func (r *testRepository) Delete(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, key)
	return nil
}

func (r *testRepository) DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	deleted := 0
	for k, v := range r.data {
		if deleted >= limit {
			break
		}
		if v.IsExpired(now) {
			delete(r.data, k)
			deleted++
		}
	}
	return deleted, nil
}

func (r *testRepository) CountActive(ctx context.Context, now time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, v := range r.data {
		if v.RevokedAt == nil && !v.IsExpired(now) && v.RemainingUsage > 0 {
			count++
		}
	}
	return count, nil
}
