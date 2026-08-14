//usr/bin/env go run -tags=mail_ocr "$0" "$@"; exit
//go:build mail_ocr
//
// bin/mail/ocr.go - OCR an image or scanned PDF (tesseract eng+deu).
//
//	./bin/mail/ocr.go scan.png
//	./bin/mail/ocr.go scan.pdf
//	OCR_ENGINE=paddle ./bin/mail/ocr.go scan.png
//
// PDFs try pdftotext -layout first; empty text layer uses pdftoppm + tesseract.
// No gocv. Tesseract CGO bindings are not used (D21 Zig owns Ladybug CGO).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/eSlider/2dph/internal/ocr"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, `usage: bin/mail/ocr.go <image|pdf>`)
		return 2
	}
	path := args[0]
	var (
		text string
		err  error
	)
	if strings.HasSuffix(strings.ToLower(path), ".pdf") {
		text, err = ocr.PDFFile(path)
	} else {
		text, err = ocr.ImageFile(path)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail/ocr: %v\n", err)
		return 1
	}
	fmt.Println(text)
	return 0
}
