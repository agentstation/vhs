package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSVGFont(t *testing.T) {
	for _, tc := range []struct {
		extension string
		mime      string
		format    string
	}{
		{".woff2", "font/woff2", "woff2"},
		{".woff", "font/woff", "woff"},
		{".ttf", "font/ttf", "truetype"},
		{".otf", "font/otf", "opentype"},
	} {
		t.Run(tc.extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "font"+tc.extension)
			data := []byte{0, 1, 2, 3}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			font, err := readSVGFont(path)
			if err != nil {
				t.Fatal(err)
			}
			if font.mime != tc.mime || font.format != tc.format || font.data != base64.StdEncoding.EncodeToString(data) {
				t.Fatalf("font = %#v, want exact file bytes and declared format", font)
			}
		})
	}
}

func TestReadSVGFontErrors(t *testing.T) {
	for _, path := range []string{"font.txt", filepath.Join(t.TempDir(), "missing.woff2")} {
		if _, err := readSVGFont(path); err == nil {
			t.Errorf("font path %q returned success", path)
		}
	}
	path := filepath.Join(t.TempDir(), "empty.woff2")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSVGFont(path); err == nil {
		t.Fatal("empty font returned success")
	}
}

func TestSVGEmbedsExactCaptureFont(t *testing.T) {
	opts := createTestSVGConfig()
	opts.FontFamily = captureFontFamily
	opts.FontData = "AAECAw=="
	opts.FontMIME = "font/woff2"
	opts.FontFormat = "woff2"
	gen := NewSVGGenerator(opts)
	content := gen.Generate()
	if !strings.Contains(content, `@font-face { font-family: "VHS Capture Font"; src: url("data:font/woff2;base64,AAECAw==") format("woff2"); }`) {
		t.Fatal("SVG does not embed the exact capture font")
	}
	if !strings.Contains(content, "font-family: VHS Capture Font, monospace") {
		t.Fatal("SVG text does not use the embedded font")
	}
}
