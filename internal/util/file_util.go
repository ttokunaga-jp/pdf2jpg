package util

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const tempDir = "/tmp"

// SaveUploadedFile persists the multipart file to a temporary PDF file and validates basic properties.
func SaveUploadedFile(src multipart.File, originalName string) (string, error) {
	if src == nil {
		return "", errors.New("missing file")
	}

	ext := strings.ToLower(filepath.Ext(originalName))
	if ext != ".pdf" {
		return "", fmt.Errorf("invalid file extension: %s", ext)
	}

	tempFile, err := os.CreateTemp(tempDir, "pdf2jpg-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	defer func() {
		if err != nil {
			tempFile.Close()
			_ = os.Remove(tempFile.Name())
		}
	}()

	header := make([]byte, 512)
	n, readErr := io.ReadFull(src, header)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
		return "", fmt.Errorf("read file header: %w", readErr)
	}

	contentType := http.DetectContentType(header[:n])
	if contentType != "application/pdf" && contentType != "application/octet-stream" {
		return "", fmt.Errorf("invalid content type: %s", contentType)
	}

	if _, err = tempFile.Write(header[:n]); err != nil {
		return "", fmt.Errorf("write header: %w", err)
	}

	if _, err = io.Copy(tempFile, src); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	if err = tempFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	if seeker, ok := src.(io.Seeker); ok {
		_, _ = seeker.Seek(0, io.SeekStart)
	}

	return tempFile.Name(), nil
}

// RemoveFile attempts to delete the file at the specified path.
func RemoveFile(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}
