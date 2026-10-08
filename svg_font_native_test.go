package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/agentstation/vhs/parser"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func TestAutomaticSVGFontNative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell font tool fixtures require Unix")
	}
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 to run this test")
	}
	t.Setenv("VHS_SVG_FONT_FILE", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "faces.ttc")
	if err := os.WriteFile(path, testFontCollection(gomono.TTF, goregular.TTF), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VHS_TEST_FONT_PATH", path)
	writeFontTool(t, dir, "fc-match", `case "$3" in
"Go Mono") printf '%s\n0\nGo Mono\n' "$VHS_TEST_FONT_PATH";;
"Go") printf '%s\n1\nGo\n' "$VHS_TEST_FONT_PATH";;
*) printf '%s\n0\nFallback\n' "$VHS_TEST_FONT_PATH";;
esac`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, mode := range []string{"automatic", "flag", "environment"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			v := New()
			t.Cleanup(func() { _ = v.Cleanup() })
			v.Options.Shell = Shell{Command: []string{"bash"}}
			v.Options.Video.Output.SVG = filepath.Join(dir, "automatic.svg")
			if mode != "automatic" {
				explicitPath := filepath.Join(dir, "GoMono.ttf")
				if err := os.WriteFile(explicitPath, gomono.TTF, 0o600); err != nil {
					t.Fatal(err)
				}
				if mode == "flag" {
					v.Options.SVG.FontFile = explicitPath
					t.Setenv("VHS_SVG_FONT_FILE", "ignored-missing.ttf")
				} else {
					t.Setenv("VHS_SVG_FONT_FILE", explicitPath)
				}
			}
			v.Options.Video.Style.Columns, v.Options.Video.Style.Rows = 30, 8
			v.Options.FontSize = 20
			v.Options.Video.Style.WindowBar = "Colorful"
			v.Options.Video.Style.WindowBarTitle = "Wimé"
			v.Options.Video.Style.WindowBarFontFamily = "Go"
			if err := v.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = v.terminate() }()
			if err := v.Page.Wait(rod.Eval("() => window.term != undefined")); err != nil {
				t.Fatal(err)
			}
			if err := ExecuteSetFontFamily(parser.Command{Args: `Bad'; globalThis.vhsFontInjected=true; //`}, &v); err != nil {
				t.Fatalf("font name broke JavaScript: %v", err)
			}
			check, err := v.Page.Eval("() => globalThis.vhsFontInjected === undefined")
			if err != nil || !check.Value.Bool() {
				t.Fatal("font name executed JavaScript")
			}
			if err := ExecuteSetFontFamily(parser.Command{Args: "Go Mono"}, &v); err != nil {
				t.Fatal(err)
			}
			if err := v.installSVGFont(); err != nil {
				t.Fatal(err)
			}
			if mode != "automatic" && v.svgFont.data != base64.StdEncoding.EncodeToString(gomono.TTF) {
				t.Fatal("explicit font bytes changed")
			}
			if err := v.Setup(); err != nil {
				t.Fatal(err)
			}
			metrics, err := v.Page.Eval(`() => {
		const context = document.createElement('canvas').getContext('2d');
		const measure = family => { context.font = '20px "' + family + '"'; return context.measureText('WimΩé').width; };
		return { family: term.options.fontFamily, cols: term.cols, rows: term.rows,
			main: measure('VHS Capture Font'), title: measure('VHS Title Font') };
	}`)
			if err != nil {
				t.Fatal(err)
			}
			if metrics.Value.Get("family").Str() != captureFontFamily || metrics.Value.Get("cols").Int() != 30 || metrics.Value.Get("rows").Int() != 8 {
				t.Fatalf("automatic font was not installed before grid measurement: %s", metrics.Value.JSON("", ""))
			}
			if metrics.Value.Get("main").Num() == metrics.Value.Get("title").Num() {
				t.Fatal("collection title face reused the terminal face")
			}
			if _, err := v.Page.Eval("text => term.write(text)", "WimΩé"); err != nil {
				t.Fatal(err)
			}
			recordCtx, stop := context.WithCancel(ctx)
			results := v.Record(recordCtx)
			time.Sleep(250 * time.Millisecond)
			stop()
			for err := range results {
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := v.Render(ctx); err != nil {
				t.Fatal(err)
			}
			if mode == "automatic" && v.Options.Video.Style.FontFamily != "Go Mono" {
				t.Fatal("automatic capture changed raster title family")
			}
			data, err := os.ReadFile(v.Options.Video.Output.SVG)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), "@font-face") != 2 {
				t.Fatal("automatic SVG omitted a selected terminal or title face")
			}
			page, err := v.browser.Page(proto.TargetCreateTarget{URL: (&url.URL{Scheme: "file", Path: v.Options.Video.Output.SVG}).String()})
			if err != nil {
				t.Fatal(err)
			}
			if err := page.WaitLoad(); err != nil {
				t.Fatal(err)
			}
			rendered, err := page.Eval(`async () => {
		await document.fonts.load('20px "VHS Capture Font"', 'WimΩé');
		await document.fonts.load('20px "VHS Title Font"', 'Wimé');
		const context = document.createElementNS('http://www.w3.org/1999/xhtml', 'canvas').getContext('2d');
		const measure = (family, text) => { context.font = '20px "' + family + '"'; return context.measureText(text).width; };
		const title = [...document.querySelectorAll('text')].find(text => text.textContent === 'Wimé');
		return { main: measure('VHS Capture Font', 'WimΩé'), title: measure('VHS Title Font', 'Wimé'),
			titleFamily: title.getAttribute('font-family') };
	}`)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(rendered.Value.Get("main").Num()-metrics.Value.Get("main").Num()) > .01 {
				t.Fatalf("capture and SVG terminal advances differ: capture=%s SVG=%s", metrics.Value.JSON("", ""), rendered.Value.JSON("", ""))
			}
			if !strings.Contains(rendered.Value.Get("titleFamily").Str(), titleFontFamily) {
				t.Fatal("separate title family is not embedded")
			}
			titleWidth, err := v.Page.Eval(`() => { const context = document.createElement('canvas').getContext('2d'); context.font = '20px "VHS Title Font"'; return context.measureText('Wimé').width; }`)
			if err != nil || math.Abs(titleWidth.Value.Num()-rendered.Value.Get("title").Num()) > .01 {
				t.Fatalf("capture and SVG title advances differ: %v", err)
			}
			t.Log(fmt.Sprintf("capture/SVG main advance %.4fpx; title %.4fpx; portable SVG %d bytes", metrics.Value.Get("main").Num(), rendered.Value.Get("title").Num(), len(data)))
		})
	}
}

func TestEvaluateAutomaticSVGFontSubset(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 on Unix to run this test")
	}
	if _, err := exec.LookPath("pyftsubset"); err != nil {
		t.Skip("optional FontTools is unavailable")
	}
	t.Setenv("VHS_SVG_FONT_FILE", "")
	dir := t.TempDir()
	fontPath := filepath.Join(dir, "GoMono.ttf")
	if err := os.WriteFile(fontPath, gomono.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VHS_TEST_FONT_PATH", fontPath)
	writeFontTool(t, dir, "fc-match", `printf '%s\n0\nGo Mono\n' "$VHS_TEST_FONT_PATH"`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output := filepath.Join(dir, "evaluated.svg")
	tape := fmt.Sprintf("Output %q\nSet Shell bash\nSet Width 480\nSet Height 240\nSet FontFamily \"Go Mono\"\nSet FontSize 20\nSet TypingSpeed 1ms\nSet WindowBar Colorful\nSet WindowBarTitle \"é\"\nType \"printf 'WimΩ'\"\nEnter\nSleep 250ms\n", filepath.ToSlash(output))
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if errs := Evaluate(ctx, tape, io.Discard); len(errs) != 0 {
		t.Fatal(errs)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`data:font/woff2;base64,([A-Za-z0-9+/=]+)`).FindSubmatch(data)
	if len(match) != 2 || len(match[1]) >= len(base64.StdEncoding.EncodeToString(gomono.TTF)) {
		t.Fatal("Evaluate did not retain a smaller portable font after capture teardown")
	}
	font := svgFont{data: string(match[1]), mime: "font/woff2", format: "woff2"}
	checkSubsetGlyphs(t, font, "WimΩé")
	t.Logf("Evaluate portable SVG %d bytes; embedded font base64 %d bytes, full font base64 %d", len(data), len(match[1]), len(base64.StdEncoding.EncodeToString(gomono.TTF)))
}
