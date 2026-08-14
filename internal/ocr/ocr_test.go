package ocr

import (
	"image"
	"image/color"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrayContrastStretches(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	img.SetGray(0, 0, color.Gray{Y: 64})
	img.SetGray(0, 1, color.Gray{Y: 64})
	img.SetGray(1, 0, color.Gray{Y: 64})
	img.SetGray(1, 1, color.Gray{Y: 192})
	out := GrayContrast(img).(*image.Gray)
	if out.GrayAt(0, 0).Y != 0 {
		t.Fatalf("min should map to 0, got %d", out.GrayAt(0, 0).Y)
	}
	if out.GrayAt(1, 1).Y != 255 {
		t.Fatalf("max should map to 255, got %d", out.GrayAt(1, 1).Y)
	}
}

func TestHelloPNGFixtureOCR(t *testing.T) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		t.Skip("tesseract not installed")
	}
	path := filepath.Join("testdata", "hello.png")
	got, err := ImageFile(path)
	if err != nil {
		t.Fatal(err)
	}
	up := strings.ToUpper(got)
	if !strings.Contains(up, "HELLO") {
		t.Fatalf("ocr %q missing HELLO", got)
	}
}

func TestPaddleEngineUsesPaddleocrBinary(t *testing.T) {
	t.Setenv("OCR_ENGINE", "paddle")
	_, err := ImageFile(filepath.Join("testdata", "hello.png"))
	if _, look := exec.LookPath("paddleocr"); look != nil {
		if err == nil {
			t.Fatal("expected error when paddleocr is missing")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}
