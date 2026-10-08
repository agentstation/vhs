package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func writeFontTool(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestSVGFontFamilyInjection(t *testing.T) {
	family := `Evil"; } .injected { display:none } /*<&`
	opts := createTestSVGConfig()
	opts.FontFamily = family
	opts.Style.WindowBar = "Colorful"
	opts.Style.WindowBarTitle = "Title"
	opts.Style.WindowBarFontFamily = family
	data := NewSVGGenerator(opts).Generate()
	decoder := xml.NewDecoder(strings.NewReader(data))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("invalid XML: %v", err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "text" {
			for _, attr := range start.Attr {
				if attr.Name.Local == "font-family" && !strings.Contains(attr.Value, `\;`) {
					t.Fatalf("title font has unquoted CSS metacharacters: %q", attr.Value)
				}
			}
		}
	}
	if strings.Contains(data, family) || strings.Contains(data, ".injected { display:none }") {
		t.Fatal("font family inserted active CSS")
	}
}

func TestSVGEmbeddedFontWithFallbackStack(t *testing.T) {
	opts := createTestSVGConfig()
	opts.FontFamily = "Named Font, monospace"
	opts.FontData = base64.StdEncoding.EncodeToString(gomono.TTF)
	opts.FontMIME, opts.FontFormat = "font/ttf", "truetype"
	opts.Style.FontFamily = opts.FontFamily
	opts.Style.WindowBar, opts.Style.WindowBarTitle = "Colorful", "Title"
	data := NewSVGGenerator(opts).Generate()
	if !strings.Contains(data, `font-family: "VHS Capture Font"; src:`) || !strings.Contains(data, "font-family: VHS Capture Font, monospace;") || strings.Contains(data, "Named Font") {
		t.Fatal("embedded bytes do not select the same face for a fallback-stack configuration")
	}
	if opts.Style.FontFamily != "Named Font, monospace" {
		t.Fatal("SVG generator mutated caller font style")
	}
}

func TestAutomaticSVGFontRetainsRasterTitleFamily(t *testing.T) {
	v := New()
	t.Cleanup(func() { _ = v.Cleanup() })
	v.Options.SVG.OptimizeSize = false
	v.Options.Video.Output.SVG = filepath.Join(t.TempDir(), "title.svg")
	v.Options.FontFamily = captureFontFamily
	v.svgOriginalFamily = "Go Mono"
	v.svgFont = svgFont{data: base64.StdEncoding.EncodeToString(gomono.TTF), mime: "font/ttf", format: "truetype", auto: true}
	v.svgFrames = []SVGFrame{{Lines: []string{"Hello"}}}
	v.duration = time.Second
	v.Options.Video.Style.WindowBar, v.Options.Video.Style.WindowBarTitle = "Colorful", "Title"
	if err := v.Render(t.Context()); err != nil {
		t.Fatal(err)
	}
	if family := getWindowBarFontFamily(v.Options.Video.Style, ""); family != "Go Mono, monospace" {
		t.Fatalf("raster title family became a browser-only alias: %q", family)
	}
	data, err := os.ReadFile(v.Options.Video.Output.SVG)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`font-family="VHS Capture Font, monospace"`)) {
		t.Fatal("SVG title did not select the embedded capture face")
	}
}

func TestRasterOnlySkipsAutomaticSVGFonts(t *testing.T) {
	v := New()
	t.Cleanup(func() { _ = v.Cleanup() })
	v.Options.Video.Output.GIF = "capture.gif"
	v.Options.SVG.FontFile = "missing.ttf"
	v.Options.FontFamily = "Named Font"
	if err := v.installSVGFont(); err != nil || v.Options.FontFamily != "Named Font" {
		t.Fatalf("raster-only capture changed font selection: %v", err)
	}
}

func TestSVGFontDiscoveryAndPrecedence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell font tool fixtures require Unix")
	}
	dir := t.TempDir()
	fontPath := filepath.Join(dir, "GoMono.ttf")
	if err := os.WriteFile(fontPath, gomono.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("VHS_TEST_FONT_PATH", fontPath)
	for _, family := range []string{"", "monospace", "sans-serif", "system-ui", defaultFontFamily, "Go Mono, monospace"} {
		font, err := discoverSVGFont(t.Context(), family)
		if err != nil || font.data != "" {
			t.Fatalf("default family %q: %v %v", family, font, err)
		}
	}
	font, err := selectSVGFont(t.Context(), fontPath, "Named Font")
	if err != nil || font.auto || font.data != base64.StdEncoding.EncodeToString(gomono.TTF) {
		t.Fatalf("explicit path changed exact bytes: %v %v", font.auto, err)
	}
	font, err = discoverSVGFont(t.Context(), "Go Mono")
	if err != nil || font.data != "" {
		t.Fatalf("missing optional tool: %v %v", font, err)
	}
	writeFontTool(t, dir, "fc-match", "exit 1")
	font, err = discoverSVGFont(t.Context(), "Go Mono")
	if err != nil || font.data != "" {
		t.Fatalf("failed optional tool: %v %v", font, err)
	}
	writeFontTool(t, dir, "fc-match", `printf '%s\n0\nGo Mono\n' "$VHS_TEST_FONT_PATH"`)
	font, err = discoverSVGFont(t.Context(), "Go Mono")
	if err != nil || !font.auto || font.data != base64.StdEncoding.EncodeToString(gomono.TTF) {
		t.Fatalf("automatic discovery: %v %v", font.auto, err)
	}
	font, err = discoverSVGFont(t.Context(), "Unknown Font")
	if err != nil || font.data != "" {
		t.Fatalf("unknown name embedded a different family: %v %v", font, err)
	}
	for _, metadata := range []string{"missing-fields", "path\ninvalid\nGo Mono\n", "path\n-1\nGo Mono\n", "path\n65536\nGo Mono\n"} {
		t.Setenv("VHS_TEST_FONT_METADATA", metadata)
		writeFontTool(t, dir, "fc-match", `printf '%s' "$VHS_TEST_FONT_METADATA"`)
		if _, err := discoverSVGFont(t.Context(), "Go Mono"); err == nil {
			t.Fatal("invalid resolved metadata accepted")
		}
	}
	writeFontTool(t, dir, "fc-match", `printf '%s\n0\nGo Mono\n' "$VHS_TEST_FONT_PATH"`)
	t.Setenv("VHS_TEST_FONT_PATH", filepath.Join(dir, "missing.ttf"))
	if _, err := discoverSVGFont(t.Context(), "Go Mono"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bad resolved path error: %v", err)
	}
	writeFontTool(t, dir, "fc-match", "exec /bin/sleep 10")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := discoverSVGFont(ctx, "Go Mono"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("discovery ignored capture cancellation: %v", err)
	}
}

func TestSVGFontSubsetFallbacks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell font tool fixtures require Unix")
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	font := svgFont{data: base64.StdEncoding.EncodeToString(gomono.TTF), mime: "font/ttf", format: "truetype", auto: true}
	for _, body := range []string{"", "exit 1", `for arg; do case "$arg" in --output-file=*) printf junk > "${arg#*=}";; esac; done`} {
		if body != "" {
			writeFontTool(t, dir, "pyftsubset", body)
		}
		result, err := subsetSVGFont(t.Context(), font, "Hello Ω")
		if err != nil || result != font {
			t.Fatalf("optional subset failure changed font: %v %v", result == font, err)
		}
	}
	writeFontTool(t, dir, "pyftsubset", "exec /bin/sleep 10")
	explicit := font
	explicit.auto = false
	result, err := subsetSVGFont(t.Context(), explicit, "Hello")
	if err != nil || result != explicit {
		t.Fatal("explicit file went through automatic subset")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := subsetSVGFont(ctx, font, "Hello"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("subsetting ignored capture cancellation: %v", err)
	}
}

func testFontCollection(fonts ...[]byte) []byte {
	headerSize := 12 + 4*len(fonts)
	data := make([]byte, headerSize)
	copy(data, "ttcf")
	binary.BigEndian.PutUint32(data[4:8], 0x10000)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(fonts)))
	for index, input := range fonts {
		offset := len(data)
		binary.BigEndian.PutUint32(data[12+4*index:], uint32(offset))
		face := bytes.Clone(input)
		count := int(binary.BigEndian.Uint16(face[4:6]))
		for i := range count {
			record := face[12+16*i : 28+16*i]
			binary.BigEndian.PutUint32(record[8:12], binary.BigEndian.Uint32(record[8:12])+uint32(offset))
		}
		data = append(data, face...)
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return data
}

func TestSVGFontCollectionSelectsFace(t *testing.T) {
	collection := testFontCollection(gomono.TTF, goregular.TTF)
	for index, original := range [][]byte{gomono.TTF, goregular.TTF} {
		data, err := extractSVGFontFace(collection, index)
		if err != nil {
			t.Fatal(err)
		}
		selected, err := sfnt.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		want, err := sfnt.Parse(original)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range "WimΩé" {
			g, _ := selected.GlyphIndex(nil, r)
			w, _ := want.GlyphIndex(nil, r)
			advance, _ := selected.GlyphAdvance(nil, g, fixed.I(20), 0)
			wantAdvance, _ := want.GlyphAdvance(nil, w, fixed.I(20), 0)
			if advance != wantAdvance || g != w {
				t.Fatalf("face %d glyph %q: glyph/advance differs", index, r)
			}
		}
		var checksum uint32
		for i := 0; i < len(data); i += 4 {
			checksum += binary.BigEndian.Uint32(data[i:])
		}
		if checksum != 0xb1b0afba {
			t.Fatalf("extracted face checksum=%x", checksum)
		}
	}
	for _, tc := range []struct {
		data  []byte
		index int
	}{{collection, -1}, {collection, 2}, {collection[:15], 0}, {[]byte("ttcf"), 0}} {
		if _, err := extractSVGFontFace(tc.data, tc.index); err == nil {
			t.Fatal("invalid collection accepted")
		}
	}
}

func TestSVGFontSubsetIncludesTitleAndCursor(t *testing.T) {
	if _, err := exec.LookPath("pyftsubset"); err != nil {
		t.Skip("optional FontTools is unavailable")
	}
	font := svgFont{data: base64.StdEncoding.EncodeToString(gomono.TTF), mime: "font/ttf", format: "truetype", auto: true}
	v := New()
	t.Cleanup(func() { _ = v.Cleanup() })
	v.svgFont, v.svgTitleFont = font, font
	v.svgFrames = []SVGFrame{{Lines: []string{"Hello"}, CursorChar: "Ω"}}
	v.Options.Video.Style.WindowBar = "Colorful"
	v.Options.Video.Style.WindowBarTitle = "é"
	main, _, err := v.outputSVGFonts(t.Context())
	if err != nil || main.format != "woff2" || len(main.data) >= len(font.data) {
		t.Fatalf("real subset failed: format=%s size=%d/%d err=%v", main.format, len(main.data), len(font.data), err)
	}
	checkSubsetGlyphs(t, main, "HeloΩé")
	v.Options.Video.Style.WindowBarFontFamily = "Go Mono"
	main, title, err := v.outputSVGFonts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkSubsetGlyphs(t, main, "HeloΩ")
	checkSubsetGlyphs(t, title, "é")
	v.Options.SVG.OptimizeSize = false
	main, title, err = v.outputSVGFonts(t.Context())
	if err != nil || main != font || title != font {
		t.Fatal("disabled optimization changed font bytes")
	}
}

func checkSubsetGlyphs(t *testing.T, font svgFont, glyphs string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "subset.woff2")
	data, err := base64.StdEncoding.DecodeString(font.data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "ttx", "-q", "-t", "cmap", "-o", "-", path)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("subset lost displayed glyphs: %v: %s", err, out)
	}
	for _, r := range glyphs {
		if !bytes.Contains(out, []byte(fmt.Sprintf(`code="0x%x"`, r))) {
			t.Fatalf("subset lost glyph %q", r)
		}
	}
}
