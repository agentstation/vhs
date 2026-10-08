package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSVGLineHeightMetrics(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		captured, lineHeight float64
		want                 float64
	}{
		{"captured spacing", 30, 1.5, 30},
		{"fallback spacing", 0, 1.5, 36},
		{"fallback default", 0, 0, 24},
		{"fallback negative", 0, -1, 24},
		{"fallback nonfinite spacing", 0, math.NaN(), 24},
		{"fallback invalid metric", math.NaN(), 1.5, 36},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := createTestSVGConfig()
			opts.FontSize = 20
			opts.LineHeight = tc.lineHeight
			opts.Frames = []SVGFrame{{Lines: []string{"ROW1", "ROW2", "ROW3"}, CharHeight: tc.captured}}
			gen := NewSVGGenerator(opts)
			if gen.charHeight != tc.want {
				t.Errorf("cell height=%g, want %g", gen.charHeight, tc.want)
			}
			assertSVGRowSpacing(t, gen.Generate(), tc.want, "")
		})
	}
}

func TestSVGLineHeightCapturedRows(t *testing.T) {
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 for native line-height capture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output := filepath.Join(t.TempDir(), "rows.svg")
	tape := fmt.Sprintf("Output %q\nSet Shell bash\nSet Width 400\nSet Height 320\nSet Padding 10\nSet FontSize 20\nSet LineHeight 1.5\nSet TypingSpeed 0ms\nSet Theme {\"foreground\":\"#abcdef\",\"background\":\"#010203\",\"red\":\"#fa0102\"}\nType `printf '\\033[2J\\033[H\\033[41mROW1\\nROW2\\nROW3\\033[0m\\n'`\nEnter\nWait+Line />$/\nSleep 500ms\n", filepath.ToSlash(output))
	var captured *VHS
	if errs := Evaluate(ctx, tape, io.Discard, func(v *VHS) { captured = v }); len(errs) != 0 {
		t.Fatalf("Evaluate failed: %v", errs)
	}
	if len(captured.svgFrames) == 0 {
		t.Fatal("no terminal frames captured")
	}
	frame := captured.svgFrames[len(captured.svgFrames)-1]
	if !strings.Contains(strings.Join(frame.Lines, "\n"), "ROW3") || !validSVGCellSize(frame.CharHeight) {
		t.Fatalf("last frame lacks colored rows or a valid cell height: %+v", frame)
	}
	opts := createTestSVGConfig()
	opts.Frames = []SVGFrame{frame}
	opts.Style = captured.Options.Video.Style
	opts.FontSize, opts.FontFamily = captured.Options.FontSize, captured.Options.FontFamily
	opts.LineHeight, opts.Theme = captured.Options.LineHeight, captured.Options.Theme
	svg := NewSVGGenerator(opts).Generate()
	assertSVGRowSpacing(t, svg, frame.CharHeight, "#fa0102")
	var root struct {
		Height int `xml:"height,attr"`
	}
	if err := xml.Unmarshal([]byte(svg), &root); err != nil {
		t.Fatal(err)
	}
	wantHeight := int(math.Ceil(float64(frame.TermRows)*frame.CharHeight)) + 2*opts.Style.Padding
	if root.Height != wantHeight {
		t.Fatalf("viewport height=%d, want captured grid height=%d", root.Height, wantHeight)
	}
}

func assertSVGRowSpacing(t *testing.T, svg string, height float64, background string) {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(svg))
	baselines := map[string]float64{}
	backgroundRows := map[int]bool{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "text":
			var text struct {
				Y     float64 `xml:"y,attr"`
				Inner string  `xml:",innerxml"`
			}
			if err := decoder.DecodeElement(&text, &start); err != nil {
				t.Fatal(err)
			}
			for _, row := range []string{"ROW1", "ROW2", "ROW3"} {
				if strings.Contains(text.Inner, row) {
					baselines[row] = text.Y
				}
			}
		case "rect":
			var rect struct {
				Y      float64 `xml:"y,attr"`
				Height float64 `xml:"height,attr"`
				Fill   string  `xml:"fill,attr"`
			}
			if err := decoder.DecodeElement(&rect, &start); err != nil {
				t.Fatal(err)
			}
			if background != "" && rect.Fill == background {
				row := int(math.Round(rect.Y / height))
				if math.Abs(rect.Y-float64(row)*height) > 0.01 || math.Abs(rect.Height-height) > 0.01 {
					t.Errorf("background y=%g height=%g does not match cell height=%g", rect.Y, rect.Height, height)
				}
				backgroundRows[row] = true
			}
		}
	}
	if len(baselines) != 3 {
		t.Fatalf("expected three text rows, got %v", baselines)
	}
	for _, pair := range [][2]string{{"ROW1", "ROW2"}, {"ROW2", "ROW3"}} {
		if delta := baselines[pair[1]] - baselines[pair[0]]; math.Abs(delta-height) > 0.01 {
			t.Errorf("%s to %s baseline delta=%g, want %g", pair[0], pair[1], delta, height)
		}
	}
	if background != "" {
		for row := range 3 {
			if !backgroundRows[row] {
				t.Errorf("missing background at terminal row %d", row)
			}
		}
	}
}
