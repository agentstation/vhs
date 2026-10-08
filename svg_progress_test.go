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
			clock, err := page.Eval(`() => {
				const bar = document.querySelector('.progress-bar');
				const slide = document.querySelector('.animation-container').getAnimations()[0];
				for (const animation of document.getAnimations()) animation.pause();
				if (!bar) return { enabled: false };
				const progress = bar.getAnimations()[0];
				const a = progress.effect.getTiming(), b = slide.effect.getTiming();
				return { enabled: true, duration: a.duration, delay: a.delay,
					slideDuration: b.duration, slideDelay: b.delay,
					fill: getComputedStyle(bar).fill, height: bar.getBBox().height,
					width: bar.getBBox().width };
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
			if duration <= 0 || duration != clock.Value.Get("slideDuration").Num() || delay != clock.Value.Get("slideDelay").Num() || math.Abs(delay+duration*.25) > 6.3 {
				t.Fatalf("animation clocks: %s", clock.Value.JSON("", ""))
			}
			if clock.Value.Get("fill").Str() != "rgb(255, 0, 0)" || clock.Value.Get("height").Num() != 1 || clock.Value.Get("width").Num() != 320 {
				t.Fatalf("bar geometry/color: %s", clock.Value.JSON("", ""))
			}
			phase := -delay / duration
			for _, sample := range []struct {
				timeFraction float64
				fillFraction float64
			}{{0, phase}, {.5, phase + .5}, {.999 - phase, .999}, {1.01 - phase, .01}} {
				geometry, err := page.Eval(`time => {
					for (const animation of document.getAnimations()) animation.currentTime = time;
					const rect = document.querySelector('.progress-bar').getBoundingClientRect();
					return { width: rect.width, y: rect.y };
				}`, duration*sample.timeFraction)
				if err != nil {
					t.Fatal(err)
				}
				wantWidth := 320 * sample.fillFraction
				if math.Abs(geometry.Value.Get("width").Num()-wantWidth) > .1 {
					t.Fatalf("time=%v: bar width=%v, want=%v", sample.timeFraction, geometry.Value.Get("width").Num(), wantWidth)
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
				for x := range 320 {
					r, g, b, _ := img.At(x, y).RGBA()
					if r > 0xf000 && g < 0x1000 && b < 0x1000 {
						redPixels++
					}
				}
				if math.Abs(float64(redPixels)-wantWidth) > 2 {
					t.Fatalf("time=%v: painted red pixels=%d, want=%v", sample.timeFraction, redPixels, wantWidth)
				}
			}
		})
	}
}
