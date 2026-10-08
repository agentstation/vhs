package main

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/gomono"
)

func TestRenderCanceledSVG(t *testing.T) {
	v := New()
	t.Cleanup(func() { _ = v.Cleanup() })
	v.Options.Video.Output.SVG = filepath.Join(t.TempDir(), "canceled.svg")
	v.svgFrames = []SVGFrame{{Lines: []string{"visible"}}}
	v.duration = time.Second
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := v.Render(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Render = %v, want cancellation", err)
	}
	if _, err := os.Stat(v.Options.Video.Output.SVG); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled render created SVG: %v", err)
	}
}

func TestBrowserGridWithExactSVGFont(t *testing.T) {
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 to run this test")
	}
	if _, err := exec.LookPath("ttyd"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		rows int
		cols int
	}{
		{"rows and columns", 10, 40},
		{"columns and pixel height", 0, 40},
		{"rows and pixel width", 10, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := New()
			t.Cleanup(func() { _ = v.Cleanup() })
			fontPath := filepath.Join(t.TempDir(), "GoMono.ttf")
			if err := os.WriteFile(fontPath, gomono.TTF, 0o600); err != nil {
				t.Fatal(err)
			}
			v.Options.SVG.FontFile = fontPath
			v.Options.Video.Output.SVG = filepath.Join(t.TempDir(), "grid.svg")
			v.Options.Video.Style.Columns = tc.cols
			v.Options.Video.Style.Rows = tc.rows
			v.Options.Video.Style.Height = 500
			v.Options.Video.Style.Width = 800
			v.Options.Video.Style.WindowBar = "Colorful"
			v.Options.Video.Style.Padding = 16
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			if err := v.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = v.terminate() }()
			if err := v.installSVGFont(); err != nil {
				t.Fatal(err)
			}
			if err := v.Setup(); err != nil {
				t.Fatal(err)
			}
			dims, err := v.Page.Eval("() => ({ cols: term.cols, rows: term.rows, font: term.options.fontFamily })")
			if err != nil {
				t.Fatal(err)
			}
			if (tc.cols > 0 && dims.Value.Get("cols").Int() != tc.cols) || (tc.rows > 0 && dims.Value.Get("rows").Int() != tc.rows) {
				t.Fatalf("terminal grid = %s", dims.Value.JSON("", ""))
			}
			if dims.Value.Get("font").Str() != captureFontFamily {
				t.Fatalf("capture font = %s", dims.Value.Get("font").Str())
			}
			if tc.rows == 0 && v.Options.Video.Style.Height != 500 {
				t.Fatalf("column sizing changed explicit height: %d", v.Options.Video.Style.Height)
			}
			if tc.cols == 0 && v.Options.Video.Style.Width != 800 {
				t.Fatalf("row sizing changed explicit width: %d", v.Options.Video.Style.Width)
			}
			recordCtx, stopRecording := context.WithCancel(ctx)
			results := v.Record(recordCtx)
			time.Sleep(250 * time.Millisecond)
			stopRecording()
			for err := range results {
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(v.svgFrames) == 0 || v.totalFrames != 0 || len(v.rasterFrames) != 0 {
				t.Fatalf("capture frames: SVG=%d raster=%d", len(v.svgFrames), v.totalFrames)
			}
			files, err := os.ReadDir(v.Options.Video.Input)
			if err != nil || len(files) != 0 {
				t.Fatalf("SVG-only capture wrote raster files: %v, %v", files, err)
			}
			if err := v.Render(ctx); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(v.Options.Video.Output.SVG)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), "data:font/ttf;base64,"+base64.StdEncoding.EncodeToString(gomono.TTF)) {
				t.Fatal("SVG omitted the exact capture font")
			}
			var dimensions struct {
				Width  int `xml:"width,attr"`
				Height int `xml:"height,attr"`
			}
			if err := xml.Unmarshal(data, &dimensions); err != nil {
				t.Fatal(err)
			}
			if dimensions.Width != v.Options.Video.Style.Width || dimensions.Height != v.Options.Video.Style.Height {
				t.Fatalf("SVG dimensions = %dx%d, want %dx%d", dimensions.Width, dimensions.Height, v.Options.Video.Style.Width, v.Options.Video.Style.Height)
			}
			cancel()
			if err := v.Setup(); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled setup = %v", err)
			}
		})
	}
}

func TestRenderCancelsActiveEncoder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell encoder process requires Unix")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	program := "#!/bin/sh\nprintf 'encoder diagnostic' >&2\nprintf started > '" + marker + "'\nexec /bin/sleep 10\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	v := New()
	t.Cleanup(func() { _ = v.Cleanup() })
	v.totalFrames = 1
	v.Options.Video.Output.GIF = filepath.Join(dir, "output.gif")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- v.Render(ctx) }()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("encoder exited before cancellation: %v", err)
		case <-deadline:
			t.Fatal("encoder did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "encoder diagnostic") {
			t.Fatalf("Render = %v, want cancellation and encoder diagnostic", err)
		}
	case <-time.After(time.Second):
		t.Fatal("encoder ignored cancellation")
	}
}

type cancelOnTypeWriter struct {
	cancel context.CancelFunc
}

func (w cancelOnTypeWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "γ") {
		w.cancel()
	}
	return len(p), nil
}

func TestEvaluateCanceledUnicodeInput(t *testing.T) {
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 to run this test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	output := filepath.Join(t.TempDir(), "output.svg")
	tape := fmt.Sprintf("Output %q\nSet Shell bash\nType \"γγγ\"\n", filepath.ToSlash(output))
	errs := Evaluate(ctx, tape, cancelOnTypeWriter{cancel})
	if len(errs) == 0 || !errors.Is(errors.Join(errs...), context.Canceled) {
		t.Fatalf("Evaluate = %v, want cancellation", errs)
	}
}
