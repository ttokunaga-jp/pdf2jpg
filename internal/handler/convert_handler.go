package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"pdf2jpg/internal/service"
	"pdf2jpg/internal/util"
)

const uploadField = "file"

// PDFConverter defines the conversion behavior required by the handler.
type PDFConverter interface {
	ConvertFirstPage(ctx context.Context, pdfPath string) ([]byte, error)
}

// ConvertHandler handles POST /convert requests.
type ConvertHandler struct {
	converter   PDFConverter
	logger      *log.Logger
	maxFileSize int64
}

// NewConvertHandler returns a configured ConvertHandler.
func NewConvertHandler(converter PDFConverter, logger *log.Logger, maxFileSize int64) http.Handler {
	return &ConvertHandler{
		converter:   converter,
		logger:      logger,
		maxFileSize: maxFileSize,
	}
}

func (h *ConvertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxFileSize)

	if err := r.ParseMultipartForm(h.maxFileSize); err != nil {
		h.handleMultipartError(w, err)
		return
	}

	file, header, err := r.FormFile(uploadField)
	if err != nil {
		h.logger.Printf("WARN: missing file field: %v", err)
		writeJSONError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	if header.Size > 0 && header.Size > h.maxFileSize {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
		writeJSONError(w, http.StatusBadRequest, "file must be a pdf")
		return
	}

	tempPath, err := util.SaveUploadedFile(file, header.Filename)
	if err != nil {
		h.logger.Printf("ERROR: saving uploaded file: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to process file")
		return
	}
	defer util.RemoveFile(tempPath)

	jpegBytes, err := h.converter.ConvertFirstPage(r.Context(), tempPath)
	if err != nil {
		h.handleConversionError(w, err)
		return
	}

	outputName := strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename)) + ".jpg"

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, outputName))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(jpegBytes); err != nil {
		h.logger.Printf("ERROR: sending jpeg response: %v", err)
	}
}

func (h *ConvertHandler) handleMultipartError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	if errors.Is(err, multipart.ErrMessageTooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	h.logger.Printf("WARN: multipart parse error: %v", err)
	writeJSONError(w, http.StatusBadRequest, "invalid multipart form data")
}

func (h *ConvertHandler) handleConversionError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrPDFHasNoPages) {
		writeJSONError(w, http.StatusBadRequest, "pdf has no pages")
		return
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeJSONError(w, http.StatusRequestTimeout, "request canceled")
		return
	}

	h.logger.Printf("ERROR: convert first page: %v", err)
	writeJSONError(w, http.StatusInternalServerError, "failed to convert pdf")
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
