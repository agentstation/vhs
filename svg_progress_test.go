package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"image/png"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentstation/vhs/lexer"
	"github.com/agentstation/vhs/parser"
	"github.com/go-rod/rod/lib/proto"
)

func TestProgressBarEscapesAttribute(t *testing.T) {
	color := `red" onload="alert(1)<&'`
	opts := createTestSVGConfig()
	opts.Style.ProgressBarColor = color
	data := NewSVGGenerator(opts).Generate()
	decoder := xml.NewDecoder(strings.NewReader(data))
	found := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid XML: %v", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "rect" {
			continue
		}
		attrs := make(map[string]string)
		for _, attr := range start.Attr {
			attrs[attr.Name.Local] = attr.Value
		}
		if attrs["class"] == "progress-bar" {
			found = true
			if attrs["fill"] != color || attrs["onload"] != "" {
				t.Fatalf("progress attributes: %v", attrs)
			}
		}
	}
	if !found {
		t.Fatal("progress rectangle missing")
	}
}

func TestProgressBarAlphaRendering(t *testing.T) {
	opts := createTestSVGConfig()
	opts.Style.Width, opts.Style.Height = 320, 200
	opts.Style.Padding, opts.Style.Margin, opts.Style.BorderRadius = 0, 0, 0
	opts.Style.WindowBar = ""
	opts.Style.ProgressBarColor = "#FF000080"
	opts.Theme.Background = "#000000"
	opts.Frames = []SVGFrame{{Lines: []string{""}, CharWidth: 8, CharHeight: 20}}
	output := filepath.Join(t.TempDir(), "alpha.svg")
	if err := os.WriteFile(output, []byte(NewSVGGenerator(opts).Generate()), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("browser blend and growth", func(t *testing.T) {
		if os.Getenv("VHS_TEST_BROWSER") == "" {
			t.Skip("set VHS_TEST_BROWSER=1 to run this test")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		browser, closeBrowser, err := startBrowser(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = closeBrowser() }()
		page, err := browser.Page(proto.TargetCreateTarget{URL: (&url.URL{Scheme: "file", Path: output}).String()})
		if err != nil {
			t.Fatal(err)
		}
		if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{Width: 340, Height: 220}); err != nil {
			t.Fatal(err)
		}
		if err := page.WaitLoad(); err != nil {
			t.Fatal(err)
		}
		result, err := page.Eval(`async () => {
			const bar = document.querySelector('.progress-bar');
			const animation = bar.getAnimations()[0];
			animation.pause(); await animation.ready;
			animation.currentTime = animation.effect.getTiming().duration / 2;
			await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
			const transform = getComputedStyle(bar).transform;
			bar.style.animation = 'none'; bar.style.transform = transform;
			await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
			const rect = bar.getBoundingClientRect();
			return {x: rect.x, y: rect.y, width: rect.width};
		}`)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(result.Value.Get("width").Num()-160) > .1 {
			t.Fatalf("half-phase width=%v, want 160", result.Value.Get("width").Num())
		}
		data, err := page.Screenshot(false, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		x, y := int(result.Value.Get("x").Num()), int(result.Value.Get("y").Num())
		r, g, b, a := img.At(x+20, y).RGBA()
		if r>>8 != 128 || g != 0 || b != 0 || a != 65535 {
			t.Fatalf("RGBA blend pixel=(%d,%d,%d,%d), want (128,0,0,255)", r>>8, g>>8, b>>8, a>>8)
		}
		r, g, b, a = img.At(x+240, y).RGBA()
		if r != 0 || g != 0 || b != 0 || a != 65535 {
			t.Fatalf("unfilled pixel=(%d,%d,%d,%d), want opaque black", r>>8, g>>8, b>>8, a>>8)
		}
	})

	t.Run("librsvg static full bar", func(t *testing.T) {
		if _, err := exec.LookPath("rsvg-convert"); err != nil {
			t.Skip("rsvg-convert is not installed")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		pngPath := filepath.Join(t.TempDir(), "static.png")
		if data, err := exec.CommandContext(ctx, "rsvg-convert", "--output", pngPath, output).CombinedOutput(); err != nil {
			t.Fatalf("rsvg-convert: %v: %s", err, data)
		}
		data, err := os.ReadFile(pngPath)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 320 || img.Bounds().Dy() != 200 {
			t.Fatalf("static bounds=%v, want 320x200", img.Bounds())
		}
		for _, x := range []int{20, 160, 300} {
			r, g, b, a := img.At(x, 199).RGBA()
			if r>>8 != 128 || g != 0 || b != 0 || a != 65535 {
				t.Fatalf("static x=%d pixel=(%d,%d,%d,%d), want (128,0,0,255)", x, r>>8, g>>8, b>>8, a>>8)
			}
		}
	})
}

func TestProgressBarCommandAndTiming(t *testing.T) {
	v := New()
	t.Cleanup(func() { _ = v.Cleanup() })
	p := parser.New(lexer.New(`Set ProgressBar "#12aB3480"`))
	commands := p.Parse()
	if len(p.Errors()) != 0 || len(commands) != 1 {
		t.Fatalf("commands=%v errors=%v", commands, p.Errors())
	}
	if err := Execute(commands[0], &v); err != nil {
		t.Fatal(err)
	}
	if v.Options.Video.Style.ProgressBarColor != "#12aB3480" {
		t.Fatal("setting did not reach video style")
	}
	for _, tc := range []struct {
		name     string
		speed    float64
		offset   float64
		duration string
		delay    string
	}{
		{"normal", 1, 0, "10s", "0s"},
		{"double and quarter offset", 2, 25, "5s", "-1.25s"},
		{"half and half offset", .5, 50, "20s", "-10s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := createTestSVGConfig()
			opts.Style = v.Options.Video.Style
			opts.Duration = 10
			opts.PlaybackSpeed = tc.speed
			opts.LoopOffset = tc.offset
			data := NewSVGGenerator(opts).Generate()
			if !strings.Contains(data, "animation: slide "+tc.duration+" step-end "+tc.delay+" infinite;") || !strings.Contains(data, "animation: progress "+tc.duration+" linear "+tc.delay+" infinite;") {
				t.Fatalf("progress and terminal clocks diverge: %s", data)
			}
		})
	}
}

func TestProgressBarNativeRendering(t *testing.T) {
	if os.Getenv("VHS_TEST_BROWSER") == "" {
		t.Skip("set VHS_TEST_BROWSER=1 to run this test")
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			output := filepath.Join(t.TempDir(), "progress.svg")
			setting := ""
			if enabled {
				setting = "Set ProgressBar \"#FF0000\"\n"
			}
			tape := fmt.Sprintf("Output %q\nSet Shell bash\nSet Width 320\nSet Height 200\nSet Padding 0\nSet TypingSpeed 1ms\nSet FontSize 16\nSet PlaybackSpeed 2\nSet LoopOffset 25\nSet CursorBlink false\n%sType \"printf ready\"\nEnter\nSleep 250ms\n", filepath.ToSlash(output), setting)
			if errs := Evaluate(ctx, tape, io.Discard); len(errs) != 0 {
				t.Fatalf("capture errors: %v", errs)
			}
			browser, closeBrowser, err := startBrowser(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = closeBrowser() }()
			page, err := browser.Page(proto.TargetCreateTarget{URL: (&url.URL{Scheme: "file", Path: output}).String()})
			if err != nil {
				t.Fatal(err)
			}
			if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{Width: 320, Height: 200}); err != nil {
				t.Fatal(err)
			}
			if err := page.WaitLoad(); err != nil {
				t.Fatal(err)
			}
			clock, err := page.Eval(`async () => {
				const bar = document.querySelector('.progress-bar');
				const slide = document.querySelector('.animation-container').getAnimations()[0];
				const animations = document.getAnimations();
				for (const animation of animations) animation.pause();
				await Promise.all(animations.map(animation => animation.ready));
				if (!bar) return { enabled: false };
				const progress = bar.getAnimations()[0];
				const a = progress.effect.getTiming(), b = slide.effect.getTiming();
				return { enabled: true, duration: a.duration, delay: a.delay,
					slideDuration: b.duration, slideDelay: b.delay,
					fill: getComputedStyle(bar).fill, height: bar.getBBox().height,
					width: bar.getBBox().width,
					viewportWidth: bar.ownerSVGElement.viewBox.baseVal.width,
					pixelWidth: bar.ownerSVGElement.width.baseVal.value };
			}`)
			if err != nil {
				t.Fatal(err)
			}
			if clock.Value.Get("enabled").Bool() != enabled {
				t.Fatalf("progress default mismatch: %s", clock.Value.JSON("", ""))
			}
			if !enabled {
				return
			}
			duration := clock.Value.Get("duration").Num()
			delay := clock.Value.Get("delay").Num()
			// Duration and delay each round to the nearest ten milliseconds.
			const roundingToleranceMS = 5 * (1 + .25)
			if duration <= 0 || duration != clock.Value.Get("slideDuration").Num() || delay != clock.Value.Get("slideDelay").Num() || math.Abs(delay+duration*.25) > roundingToleranceMS+.01 {
				t.Fatalf("animation clocks: %s", clock.Value.JSON("", ""))
			}
			if clock.Value.Get("fill").Str() != "rgb(255, 0, 0)" || clock.Value.Get("height").Num() != 1 || clock.Value.Get("width").Num() != clock.Value.Get("viewportWidth").Num() {
				t.Fatalf("bar geometry/color: %s", clock.Value.JSON("", ""))
			}
			pixelWidth := clock.Value.Get("pixelWidth").Num()
			phase := -delay / duration
			for _, sample := range []struct {
				timeFraction float64
				fillFraction float64
			}{{0, phase}, {.5, phase + .5}, {.999 - phase, .999}, {1.01 - phase, .01}} {
				geometry, err := page.Eval(`async time => {
					for (const animation of document.getAnimations()) animation.currentTime = time;
					await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
					const bar = document.querySelector('.progress-bar');
					const rect = bar.getBoundingClientRect();
					return { width: rect.width, y: rect.y, transform: getComputedStyle(bar).transform };
				}`, duration*sample.timeFraction)
				if err != nil {
					t.Fatal(err)
				}
				wantWidth := pixelWidth * sample.fillFraction
				if math.Abs(geometry.Value.Get("width").Num()-wantWidth) > .1 {
					t.Fatalf("time=%v: bar width=%v, want=%v", sample.timeFraction, geometry.Value.Get("width").Num(), wantWidth)
				}
				// Freeze the computed production transform so screenshots do not use
				// a stale compositor frame after seeking a paused CSS animation.
				_, err = page.Eval(`async transform => {
					const bar = document.querySelector('.progress-bar');
					bar.style.animation = 'none';
					bar.style.transform = transform;
					await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
				}`, geometry.Value.Get("transform").Str())
				if err != nil {
					t.Fatal(err)
				}
				data, err := page.Screenshot(false, &proto.PageCaptureScreenshot{Format: proto.PageCaptureScreenshotFormatPng})
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				redPixels := 0
				y := int(geometry.Value.Get("y").Num())
				for x := range img.Bounds().Dx() {
					r, g, b, _ := img.At(x, y).RGBA()
					if r > 0xf000 && g < 0x1000 && b < 0x1000 {
						redPixels++
					}
				}
				if math.Abs(float64(redPixels)-wantWidth) > 2 {
					t.Fatalf("time=%v: painted red pixels=%d, want=%v, transform=%s, bounds=%v, y=%d", sample.timeFraction, redPixels, wantWidth, geometry.Value.Get("transform").Str(), img.Bounds(), y)
				}
				_, err = page.Eval(`async () => {
					const bar = document.querySelector('.progress-bar');
					bar.style.removeProperty('animation');
					bar.style.removeProperty('transform');
					const animation = bar.getAnimations()[0];
					animation.pause();
					await animation.ready;
				}`)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
