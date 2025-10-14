package test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

func newTestServer(t *testing.T, opener func(string) (service.Document, error)) *httptest.Server {
	t.Helper()

	logger := log.New(io.Discard, "", 0)
	restore := service.SetDocumentOpenerForTest(opener)
	t.Cleanup(restore)

	pdfService := service.NewPDFService(defaultJPEGQual)
	convertHandler := handler.NewConvertHandler(pdfService, logger, maxUploadBytes)

	mux := http.NewServeMux()
	mux.Handle("/convert", auth.APIKeyMiddleware([]string{testAPIKey}, logger)(convertHandler))

	return httptest.NewServer(mux)
}

func TestConvertEndpoint(t *testing.T) {
	successOpener := func(string) (service.Document, error) {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: 255, A: 255})
		return &fakeDocument{
			pages: 1,
			img:   img,
		}, nil
	}

	t.Run("success", func(t *testing.T) {
		server := newTestServer(t, successOpener)
		defer server.Close()

		body, contentType := createMultipartBody(t, expectedFileName, minimalPDF())
		res := sendConvertRequest(t, server.URL, body, contentType, testAPIKey)
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
		if len(data) == 0 || data[0] != 0xFF || data[1] != 0xD8 {
			t.Fatalf("expected jpeg header, got %x", data[:2])
		}
	})

	t.Run("missing file", func(t *testing.T) {
		server := newTestServer(t, successOpener)
		defer server.Close()

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		if err := writer.Close(); err != nil {
			t.Fatalf("close writer: %v", err)
		}

		res := sendConvertRequest(t, server.URL, body, writer.FormDataContentType(), testAPIKey)
		assertJSONError(t, res, http.StatusBadRequest, "file field is required")
	})

	t.Run("invalid extension", func(t *testing.T) {
		server := newTestServer(t, successOpener)
		defer server.Close()

		body, contentType := createMultipartBody(t, "not-pdf.txt", minimalPDF())
		res := sendConvertRequest(t, server.URL, body, contentType, testAPIKey)
		assertJSONError(t, res, http.StatusBadRequest, "file must be a pdf")
	})

	t.Run("unauthorized", func(t *testing.T) {
		server := newTestServer(t, successOpener)
		defer server.Close()

		body, contentType := createMultipartBody(t, expectedFileName, minimalPDF())
		res := sendConvertRequest(t, server.URL, body, contentType, "bad-key")
		assertJSONError(t, res, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("file too large", func(t *testing.T) {
		server := newTestServer(t, successOpener)
		defer server.Close()

		oversized := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("A"), maxUploadBytes+1)...)
		body, contentType := createMultipartBody(t, expectedFileName, oversized)
		res := sendConvertRequest(t, server.URL, body, contentType, testAPIKey)
		assertJSONError(t, res, http.StatusRequestEntityTooLarge, "file too large")
	})

	t.Run("conversion failure", func(t *testing.T) {
		server := newTestServer(t, func(string) (service.Document, error) {
			return &fakeDocument{
				pages:  1,
				imgErr: assertError("render failed"),
			}, nil
		})
		defer server.Close()

		body, contentType := createMultipartBody(t, expectedFileName, minimalPDF())
		res := sendConvertRequest(t, server.URL, body, contentType, testAPIKey)
		assertJSONError(t, res, http.StatusInternalServerError, "failed to convert pdf")
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

func sendConvertRequest(t *testing.T, baseURL string, body *bytes.Buffer, contentType, apiKey string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+"/convert", body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	return res
}

func assertJSONError(t *testing.T, res *http.Response, expectedStatus int, expectedMsg string) {
	t.Helper()
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
