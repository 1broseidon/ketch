package extract

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/1broseidon/ketch/internal/failure"

	pdf "github.com/ledongthuc/pdf"
)

// ErrPDFNoText reports a valid PDF with no extractable text layer.
var ErrPDFNoText = failure.New(failure.Precondition, "pdf_no_text", "PDF contains no extractable text")

// PDFExtractor converts PDF bytes into markdown-compatible text.
type PDFExtractor interface {
	Extract(ctx context.Context, src []byte) (markdown string, err error)
}

// NewPDFExtractor returns the built-in pure-Go PDF text extractor.
func NewPDFExtractor() PDFExtractor {
	return &builtInPDFExtractor{}
}

type builtInPDFExtractor struct{}

// Extract recovers parser panics because malformed PDFs must be ordinary
// scrape errors rather than process crashes.
func (e *builtInPDFExtractor) Extract(ctx context.Context, src []byte) (markdown string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			markdown = ""
			err = fmt.Errorf("PDF extraction panicked: %v", recovered)
		}
	}()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	reader, err := pdf.NewReader(bytes.NewReader(src), int64(len(src)))
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}
	plainText, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extract PDF text: %w", err)
	}

	var output strings.Builder
	if _, err := io.Copy(&output, plainText); err != nil {
		return "", fmt.Errorf("read PDF text: %w", err)
	}
	markdown = strings.TrimSpace(output.String())
	if markdown == "" {
		return "", ErrPDFNoText
	}
	return markdown, nil
}
