package main

import (
	"context"
	"encoding/xml"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

func TestSVGGridGeometry(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		cols, rows            int
		charWidth, charHeight float64
		bar                   string
		margin                int
		width, height         int
	}{
		{"full grid", 70, 20, 8.8, 20, "Colorful", 7, 650, 464},
		{"fractional cells", 3, 3, 8.25, 20.25, "", 0, 45, 81},
		{"columns only", 70, 0, 8.8, 20, "", 0, 636, 600},
		{"rows only", 0, 20, 8.8, 20, "Colorful", 0, 1200, 450},
		{"pixel fallback", 0, 0, 8.8, 20, "", 0, 1200, 600},
		{"invalid metric", 0, 0, math.Inf(1), math.NaN(), "", 0, 1200, 600},
		{"fallback metric grid", 3, 3, math.Inf(1), math.NaN(), "", 0, 47, 78},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := createTestSVGConfig()
			opts.Style.Width, opts.Style.Height = 1200, 600
			opts.Style.Padding, opts.Style.Margin = 10, tc.margin
			opts.Style.WindowBar, opts.Style.WindowBarSize = tc.bar, 30
			opts.Style.BorderRadius = 20
			opts.Frames[0].TermCols, opts.Frames[0].TermRows = tc.cols, tc.rows
			opts.Frames[0].CharWidth, opts.Frames[0].CharHeight = tc.charWidth, tc.charHeight
			before := *opts.Style
			svg := NewSVGGenerator(opts).Generate()
			if *opts.Style != before {
				t.Fatalf("Generate changed caller style: before=%+v after=%+v", before, *opts.Style)
			}
			var root struct {
				Width  int `xml:"width,attr"`
				Height int `xml:"height,attr"`
			}
			if err := xml.Unmarshal([]byte(svg), &root); err != nil {
				t.Fatal(err)
			}
			if root.Width != tc.width || root.Height != tc.height {
				t.Fatalf("dimensions=%dx%d, want %dx%d", root.Width, root.Height, tc.width, tc.height)
			}
			if strings.Contains(strings.SplitN(svg, ">", 2)[0], "background:") {
				t.Fatal("root fills rounded corner cutouts")
			}
		})
	}
}

func TestSVGGridRoundedCornerPixels(t *testing.T) {
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 for native SVG pixels")
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
	for _, margin := range []int{0, 7} {
		opts := createTestSVGConfig()
		opts.Style.Padding, opts.Style.BorderRadius = 10, 20
		opts.Style.Margin, opts.Style.MarginFill = margin, "#112233"
		opts.Style.WindowBar = "Colorful"
		opts.Frames[0].TermCols, opts.Frames[0].TermRows = 70, 20
		svg := NewSVGGenerator(opts).Generate()
		result, err := page.Eval(`async (svg, margin) => {
   const image = new Image(); image.src = URL.createObjectURL(new Blob([svg], {type: 'image/svg+xml'})); await image.decode();
   const canvas = document.createElement('canvas'); canvas.width=image.width; canvas.height=image.height;
   const context=canvas.getContext('2d'); context.drawImage(image,0,0);
   return Array.from(context.getImageData(margin+1,margin+1,1,1).data);
  }`, svg, margin)
		if err != nil {
			t.Fatal(err)
		}
		pixel := result.Value.Arr()
		if margin == 0 && pixel[3].Int() != 0 {
			t.Fatalf("rounded corner alpha=%d", pixel[3].Int())
		}
		if margin > 0 && (pixel[0].Int() != 17 || pixel[1].Int() != 34 || pixel[2].Int() != 51 || pixel[3].Int() != 255) {
			t.Fatalf("corner lost margin fill: %v", result.Value)
		}
	}
}
