package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

func TestSVGCursorThemeColor(t *testing.T) {
	for _, tc := range []struct {
		name, cursor, foreground, want string
	}{
		{"authored cursor", "#12ab34", "#abcdef", "#12ab34"},
		{"authored cursor without foreground", "#12ab34", "", "#12ab34"},
		{"foreground fallback", "", "#abcdef", "#abcdef"},
		{"default fallback", "", "", defaultCursorColor},
	} {
		for _, character := range []string{"█", " "} {
			t.Run(tc.name+"/"+character, func(t *testing.T) {
				opts := createTestSVGConfig()
				opts.Theme.Cursor, opts.Theme.Foreground = tc.cursor, tc.foreground
				opts.CursorBlink = false
				opts.Frames = []SVGFrame{{Lines: []string{"AB"}, CursorX: 1, CursorChar: character, CharWidth: 20, CharHeight: 24}}
				svg := NewSVGGenerator(opts).Generate()
				want := fmt.Sprintf(`style="fill:%s;">%s</tspan>`, tc.want, "█")
				if !strings.Contains(svg, want) {
					t.Fatalf("cursor does not use %s", tc.want)
				}
			})
		}
	}
}

func TestSVGCursorThemePixels(t *testing.T) {
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 for native SVG cursor pixels")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	browser, closeBrowser, err := startBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBrowser()
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, cursor, foreground string
		rgb                      [3]int
	}{
		{"authored cursor", "#12ab34", "#abcdef", [3]int{18, 171, 52}},
		{"authored cursor without foreground", "#12ab34", "", [3]int{18, 171, 52}},
		{"foreground fallback", "", "#abcdef", [3]int{171, 205, 239}},
		{"default fallback", "", "", [3]int{221, 221, 221}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := createTestSVGConfig()
			opts.Theme.Cursor, opts.Theme.Foreground = tc.cursor, tc.foreground
			opts.CursorBlink = false
			opts.Frames = []SVGFrame{{Lines: []string{""}, CursorChar: "█", CharWidth: 20, CharHeight: 24}}
			assertSVGCursorPixels(t, page, NewSVGGenerator(opts).Generate(), tc.rgb)
		})
	}
}

func TestSVGCursorThemeColorEscapesAttribute(t *testing.T) {
	opts := createTestSVGConfig()
	opts.Theme.Cursor = `#12" data-probe="&<>'`
	opts.Frames = []SVGFrame{{Lines: []string{""}, CursorChar: "█"}}
	svg := NewSVGGenerator(opts).Generate()
	var root struct {
		Name xml.Name `xml:"svg"`
	}
	if err := xml.Unmarshal([]byte(svg), &root); err != nil {
		t.Fatalf("authored cursor color produced invalid XML: %v", err)
	}
	if !strings.Contains(svg, `style="fill:`+html.EscapeString(opts.Theme.Cursor)+`;"`) {
		t.Fatal("cursor color does not use XML attribute escaping")
	}
}

func TestSVGCursorCapturedThemePixels(t *testing.T) {
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 for native terminal capture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output := filepath.Join(t.TempDir(), "cursor.svg")
	tape := fmt.Sprintf(`Output %q
Set Shell bash
Set Width 320
Set Height 200
Set FontSize 24
Set CursorBlink false
Set Theme {"foreground":"#abcdef","background":"#010203","cursor":"#12ab34"}
Sleep 500ms
`, filepath.ToSlash(output))
	if errs := Evaluate(ctx, tape, io.Discard); len(errs) != 0 {
		t.Fatalf("Evaluate failed: %v", errs)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	browser, closeBrowser, err := startBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBrowser()
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		t.Fatal(err)
	}
	assertSVGCursorPixels(t, page, string(content), [3]int{18, 171, 52})
}

func assertSVGCursorPixels(t *testing.T, page *rod.Page, svg string, rgb [3]int) {
	t.Helper()
	result, err := page.Eval(`async (svg, rgb) => {
  const url = URL.createObjectURL(new Blob([svg], {type: 'image/svg+xml'}));
  try {
    const image = new Image(); image.src = url; await image.decode();
    const canvas = document.createElement('canvas'); canvas.width = image.width; canvas.height = image.height;
    const context = canvas.getContext('2d'); context.drawImage(image, 0, 0);
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height).data;
    let count = 0;
    for (let i = 0; i < pixels.length; i += 4) {
      if (pixels[i] === rgb[0] && pixels[i+1] === rgb[1] && pixels[i+2] === rgb[2] && pixels[i+3] === 255) count++;
    }
    return count;
  } finally { URL.revokeObjectURL(url); }
}`, svg, rgb)
	if err != nil {
		t.Fatal(err)
	}
	if result.Value.Int() == 0 {
		t.Fatalf("SVG has no opaque cursor pixels with RGB %v", rgb)
	}
}
