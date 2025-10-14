package service

import (
	"context"
	"errors"
	"image"
	"image/color"
	"testing"
)

type stubDocument struct {
	pages  int
	img    image.Image
	imgErr error
}

func (s *stubDocument) NumPage() int                              { return s.pages }
func (s *stubDocument) Image(pageNumber int) (image.Image, error) { return s.img, s.imgErr }
func (s *stubDocument) Close() error                              { return nil }

func TestConvertFirstPage_Success(t *testing.T) {
	restore := SetDocumentOpenerForTest(func(string) (Document, error) {
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		return &stubDocument{
			pages: 1,
			img:   img,
		}, nil
	})
	defer restore()

	svc := NewPDFService(85)
	result, err := svc.ConvertFirstPage(context.Background(), "ignored")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected jpeg bytes, got empty slice")
	}
	if result[0] != 0xFF || result[1] != 0xD8 {
		t.Fatalf("expected jpeg header, got %x", result[:2])
	}
}

func TestConvertFirstPage_NoPages(t *testing.T) {
	restore := SetDocumentOpenerForTest(func(string) (Document, error) {
		return &stubDocument{pages: 0}, nil
	})
	defer restore()

	svc := NewPDFService(85)
	_, err := svc.ConvertFirstPage(context.Background(), "ignored")
	if !errors.Is(err, ErrPDFHasNoPages) {
		t.Fatalf("expected ErrPDFHasNoPages, got %v", err)
	}
}

func TestConvertFirstPage_OpenError(t *testing.T) {
	openErr := errors.New("open failure")
	restore := SetDocumentOpenerForTest(func(string) (Document, error) {
		return nil, openErr
	})
	defer restore()

	svc := NewPDFService(85)
	_, err := svc.ConvertFirstPage(context.Background(), "ignored")
	if err == nil || !errors.Is(err, openErr) {
		t.Fatalf("expected wrapped open error, got %v", err)
	}
}

func TestConvertFirstPage_ContextCanceled(t *testing.T) {
	restore := SetDocumentOpenerForTest(func(string) (Document, error) {
		t.Fatal("document opener should not be called when context is canceled")
		return nil, nil
	})
	defer restore()

	svc := NewPDFService(85)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.ConvertFirstPage(ctx, "ignored")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
