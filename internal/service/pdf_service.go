package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"

	fitz "github.com/gen2brain/go-fitz"
)

var (
	// ErrPDFHasNoPages is returned when the PDF contains no pages.
	ErrPDFHasNoPages = errors.New("pdf has no pages")
)

// PDFService performs PDF to JPEG conversions using go-fitz.
type Document interface {
	NumPage() int
	Image(pageNumber int) (image.Image, error)
	Close() error
}

var openDocument = func(path string) (Document, error) {
	return fitz.New(path)
}

type PDFService struct {
	jpegQuality int
}

// NewPDFService constructs a new service configured with the provided JPEG quality.
func NewPDFService(quality int) *PDFService {
	return &PDFService{
		jpegQuality: quality,
	}
}

// ConvertFirstPage renders the first page of the PDF at pdfPath to JPEG bytes.
func (s *PDFService) ConvertFirstPage(ctx context.Context, pdfPath string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	doc, err := openDocument(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer doc.Close()

	if doc.NumPage() == 0 {
		return nil, ErrPDFHasNoPages
	}

	image, err := doc.Image(0)
	if err != nil {
		return nil, fmt.Errorf("render image: %w", err)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image, &jpeg.Options{Quality: s.jpegQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}

	return buf.Bytes(), nil
}

// SetDocumentOpenerForTest allows tests to replace the document opener. It returns a restore function.
func SetDocumentOpenerForTest(opener func(string) (Document, error)) func() {
	original := openDocument
	openDocument = opener
	return func() {
		openDocument = original
	}
}
