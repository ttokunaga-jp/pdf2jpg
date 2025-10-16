package main

import (
	"bufio"
	"context"
	"errors"
	"expvar"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"

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

	if err := loadEnvFile(".env", logger); err != nil {
		logger.Fatalf("ERROR: %v", err)
	}

	apiKeys := parseAPIKeys(os.Getenv("API_KEYS"))
	if len(apiKeys) == 0 {
		logger.Fatal("missing API_KEYS environment variable")
	}

	masterKeys := parseAPIKeys(os.Getenv("MASTER_API_KEYS"))
	if len(masterKeys) == 0 {
		logger.Fatal("missing MASTER_API_KEYS environment variable")
	}

	enableFirestore := parseBoolEnv("ENABLE_FIRESTORE_KEYS", true)

	var (
		firestoreClient *firestore.Client
		keyService      *auth.KeyService
	)
	if enableFirestore {
		firestoreProject := os.Getenv("FIRESTORE_PROJECT_ID")
		if firestoreProject == "" {
			firestoreProject = os.Getenv("GOOGLE_CLOUD_PROJECT")
		}
		if firestoreProject == "" {
			logger.Fatal("missing FIRESTORE_PROJECT_ID environment variable")
		}
		firestoreCollection := os.Getenv("FIRESTORE_COLLECTION")

		ctx := context.Background()
		var err error
		firestoreClient, err = firestore.NewClient(ctx, firestoreProject)
		if err != nil {
			logger.Fatalf("ERROR: initialize firestore client: %v", err)
		}
		defer firestoreClient.Close()

		repo := auth.NewFirestoreRepository(firestoreClient, firestoreCollection)
		keyService = auth.NewKeyService(repo, logger, nil, auth.ServiceConfig{})
	} else {
		logger.Println("INFO: Firestore-backed temporary key verification disabled")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	pdfService := service.NewPDFService(jpegQuality)
	convertHandler := handler.NewConvertHandler(pdfService, logger, megabytesToBytes(maxUploadSizeMB))

	mux := http.NewServeMux()
	mux.Handle("/convert", auth.APIKeyMiddleware(auth.APIKeyMiddlewareConfig{
		StaticKeys:     apiKeys,
		KeyService:     keyService,
		Logger:         logger,
		FeatureEnabled: enableFirestore,
	})(convertHandler))

	adminHandler := buildAdminHandler(masterKeys, keyService, logger, enableFirestore)
	mux.Handle("/admin/", adminHandler)
	mux.Handle("/admin", adminHandler)

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/debug/vars", expvar.Handler())

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

func parseBoolEnv(key string, defaultVal bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultVal
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultVal
	}
	return value
}

func megabytesToBytes(mb int64) int64 {
	return mb * 1024 * 1024
}

func buildAdminHandler(masterKeys []string, keyService *auth.KeyService, logger *log.Logger, featureEnabled bool) http.Handler {
	adminMux := http.NewServeMux()
	if featureEnabled && keyService != nil {
		handler.NewKeyAdminHandler(keyService, logger).Register(adminMux)
	} else {
		adminMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "temporary key management disabled", http.StatusServiceUnavailable)
		})
	}
	return auth.AdminAuthMiddleware(auth.AdminMiddlewareConfig{
		MasterKeys: masterKeys,
		Logger:     logger,
	})(adminMux)
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

func loadEnvFile(path string, logger *log.Logger) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Printf("INFO: env file %s not found, skipping", path)
			return nil
		}
		return fmt.Errorf("open env file %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			logger.Printf("WARN: skipping malformed env line %d in %s", lineNum, path)
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set env %q from %s: %w", key, path, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read env file %q: %w", path, err)
	}

	logger.Printf("INFO: loaded environment from %s", path)
	return nil
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
