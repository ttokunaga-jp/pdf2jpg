package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pdf2jpg/internal/auth"
	"pdf2jpg/internal/handler"
	"pdf2jpg/internal/service"
)

const (
	defaultPort     = "8080"
	maxUploadSizeMB = 10
	shutdownTimeout = 10 * time.Second
	jpegQuality     = 85
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)

	apiKeys := parseAPIKeys(os.Getenv("API_KEYS"))
	if len(apiKeys) == 0 {
		logger.Fatal("missing API_KEYS environment variable")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	pdfService := service.NewPDFService(jpegQuality)
	convertHandler := handler.NewConvertHandler(pdfService, logger, megabytesToBytes(maxUploadSizeMB))

	mux := http.NewServeMux()
	mux.Handle("/convert", auth.APIKeyMiddleware(apiKeys, logger)(convertHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:    ":" + port,
		Handler: loggingMiddleware(logger)(mux),
	}

	logger.Printf("INFO: starting server on port %s", port)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Println("INFO: shutdown signal received")
	case err := <-errCh:
		logger.Fatalf("ERROR: server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("ERROR: graceful shutdown failed: %v", err)
	} else {
		logger.Println("INFO: server stopped gracefully")
	}
}

func parseAPIKeys(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	var keys []string

	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}

	return keys
}

func megabytesToBytes(mb int64) int64 {
	return mb * 1024 * 1024
}

func loggingMiddleware(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := newStatusRecorder(w)
			next.ServeHTTP(rec, r)
			logger.Printf("INFO: method=%s path=%s status=%d duration=%s", r.Method, r.URL.Path, rec.status, time.Since(start))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
