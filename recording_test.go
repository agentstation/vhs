package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRecordingTimelineHiddenSpans(t *testing.T) {
	start := time.Unix(1, 0)
	var clock recordingTimeline
	clock.resume(start)
	clock.resume(start.Add(time.Second))
	clock.pause(start.Add(2 * time.Second))
	clock.pause(start.Add(3 * time.Second))
	if got := clock.duration(start.Add(20 * time.Second)); got != 2*time.Second {
		t.Fatalf("hidden time counted: %s", got)
	}
	clock.resume(start.Add(20 * time.Second))
	if got := clock.duration(start.Add(23 * time.Second)); got != 5*time.Second {
		t.Fatalf("visible time = %s, want 5s", got)
	}
}

func TestRecordFramesSlowCapture(t *testing.T) {
	v := New()
	defer func() { _ = v.Cleanup() }()
	v.Options.Video.Framerate = 100
	v.timeline.resume(time.Now())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var timestamps []float64
	err := v.recordFrames(ctx, func(timestamp float64) error {
		timestamps = append(timestamps, timestamp)
		time.Sleep(40 * time.Millisecond)
		if len(timestamps) == 3 {
			cancel()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(timestamps) != 3 {
		t.Fatalf("captured %d frames, want 3", len(timestamps))
	}
	if elapsed := timestamps[2] - timestamps[0]; elapsed < 0.075 {
		t.Fatalf("capture timestamps compress slow capture: %vs", elapsed)
	}
}

func TestRecordFramesErrors(t *testing.T) {
	t.Run("capture failure", func(t *testing.T) {
		v := New()
		defer func() { _ = v.Cleanup() }()
		v.timeline.resume(time.Now())
		failure := errors.New("capture failed")
		err := v.recordFrames(t.Context(), func(float64) error { return failure })
		if !errors.Is(err, failure) {
			t.Fatalf("capture error = %v, want %v", err, failure)
		}
	})
	for _, rate := range []int{0, -1, int(time.Second) + 1} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			v := New()
			defer func() { _ = v.Cleanup() }()
			v.Options.Video.Framerate = rate
			if err := v.recordFrames(t.Context(), func(float64) error { t.Fatal("capture called"); return nil }); err == nil {
				t.Fatal("invalid framerate accepted")
			}
		})
	}
}

func TestRecordReportsCaptureFailure(t *testing.T) {
	v := New()
	defer func() { _ = v.Cleanup() }()
	v.Options.Video.Output.GIF = filepath.Join(t.TempDir(), "demo.gif")
	// A live owned process proves capture failure does not trigger teardown.
	v.tty = exec.Command(os.Args[0], "-test.run=^TestRecorderOwnedProcess$")
	v.tty.Env = append(os.Environ(), "VHS_TEST_KEEP_PROCESS=1")
	if err := v.tty.Start(); err != nil {
		t.Fatal(err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- v.tty.Wait() }()
	defer func() { _ = v.tty.Process.Kill() }()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch := v.Record(ctx)
	select {
	case err, open := <-ch:
		if !open || err == nil || !strings.Contains(err.Error(), "terminal canvas is unavailable") {
			t.Fatalf("recording result = %v, open = %v", err, open)
		}
	case <-time.After(time.Second):
		t.Fatal("capture error blocked without a channel reader")
	}
	for range ch {
		t.Fatal("multiple errors after terminal capture failure")
	}
	select {
	case err := <-processDone:
		t.Fatalf("capture failure terminated the evaluator's process: %v", err)
	case <-time.After(cleanupWaitTime):
	}

	cancel()
	_ = v.terminate()
	select {
	case <-processDone:
	case <-time.After(time.Second):
		t.Fatal("evaluator teardown did not terminate the owned process")
	}
}

func TestRecorderOwnedProcess(_ *testing.T) {
	if os.Getenv("VHS_TEST_KEEP_PROCESS") == "1" {
		time.Sleep(time.Hour)
	}
}

func TestPrepareRasterFramesElapsedTiming(t *testing.T) {
	v := New()
	defer func() { _ = v.Cleanup() }()
	v.Options.Video.Output.GIF = filepath.Join(t.TempDir(), "demo.gif")
	v.Options.Video.Framerate = 10
	v.rasterFrames = []float64{0.02, 0.27, 0.73}
	v.duration = time.Second
	for index := range v.rasterFrames {
		for _, format := range []string{textFrameFormat, cursorFrameFormat} {
			if err := os.WriteFile(filepath.Join(v.Options.Video.Input, fmt.Sprintf(format, index+1)), []byte(fmt.Sprint(index)), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	v.Options.Screenshot.screenshots["poster.png"] = 2
	if err := v.prepareRasterFrames(); err != nil {
		t.Fatal(err)
	}
	if v.totalFrames != 10 {
		t.Fatalf("playback frames = %d, want 10", v.totalFrames)
	}
	for i, want := range []string{"0", "0", "0", "1", "1", "1", "1", "1", "2", "2"} {
		data, err := os.ReadFile(filepath.Join(v.Options.Video.Input, fmt.Sprintf(textFrameFormat, i+1)))
		if err != nil || string(data) != want {
			t.Errorf("frame %d = %q, %v, want %q", i+1, data, err, want)
		}
	}
	data, err := os.ReadFile(filepath.Join(v.Options.Screenshot.input, fmt.Sprintf(textFrameFormat, 2)))
	if err != nil || string(data) != "1" {
		t.Errorf("screenshot = %q, %v, want original capture", data, err)
	}
}

func TestSVGOnlyAvoidsRasterPreparation(t *testing.T) {
	v := New()
	defer func() { _ = v.Cleanup() }()
	v.Options.Video.Output.SVG = filepath.Join(t.TempDir(), "demo.svg")
	v.svgFrames = []SVGFrame{{Lines: []string{"hello"}}}
	v.duration = 3 * time.Second
	v.Options.LoopOffset = 25
	if v.Options.Video.Output.needsRaster() {
		t.Fatal("SVG output requests raster capture")
	}
	if err := v.Render(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(v.Options.Video.Output.SVG)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "animation: slide 3s step-end -0.75s") {
		t.Fatal("SVG duration or loop offset does not match elapsed time")
	}
	if strings.Contains(string(data), "NaN") {
		t.Fatal("single-frame SVG contains NaN")
	}
}

func TestSVGTimelineUsesCaptureTimestamps(t *testing.T) {
	opts := createTestSVGConfig()
	opts.Frames = []SVGFrame{
		{Lines: []string{"first"}, Timestamp: 0.03},
		{Lines: []string{"second"}, Timestamp: 0.7},
		{Lines: []string{"third"}, Timestamp: 2.1},
	}
	opts.Duration = 3
	gen := NewSVGGenerator(opts)
	gen.Generate()
	for i, want := range []float64{0, 0.7 / 3 * 100, 70, 100} {
		if got := gen.timeline[i].Percentage; got != want {
			t.Errorf("keyframe %d = %v%%, want %v%%", i, got, want)
		}
	}
}

func TestCaptureBrowserPath(t *testing.T) {
	t.Setenv("VHS_BROWSER_PATH", "/env/chrome")
	if got := captureBrowserPath(""); got != "/env/chrome" {
		t.Fatalf("environment browser = %q", got)
	}
	if got := captureBrowserPath("/flag/chrome"); got != "/flag/chrome" {
		t.Fatalf("explicit browser = %q", got)
	}
}

func TestMakeSVGRequiresCapturedFrames(t *testing.T) {
	v := New()
	defer func() { _ = v.Cleanup() }()
	v.Options.Video.Output.SVG = filepath.Join(t.TempDir(), "demo.svg")
	if err := MakeSVG(&v); err == nil {
		t.Fatal("empty SVG capture returned success")
	}
}

func TestRenderReportsEncoderFailure(t *testing.T) {
	v := New()
	defer func() { _ = v.Cleanup() }()
	v.totalFrames = 1
	v.Options.Video.Output.GIF = filepath.Join(t.TempDir(), "demo.gif")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ffmpeg"), []byte("#!/bin/sh\nexit 17\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := v.Render(); err == nil {
		t.Fatal("encoder failure returned success")
	}
}

func TestSparseSVGTimelinePreservesCloseTimestamps(t *testing.T) {
	opts := createTestSVGConfig()
	opts.Duration = 100
	opts.Frames = []SVGFrame{
		{Lines: []string{"first"}, Timestamp: 0.02},
		{Lines: []string{"second"}, Timestamp: 0.04},
		{Lines: []string{"third"}, Timestamp: 0.06},
	}
	gen := NewSVGGenerator(opts)
	content := gen.Generate()
	matches := regexp.MustCompile(`(\d+(?:\.\d+)?)% \{ transform:`).FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	for _, match := range matches {
		if seen[match[1]] {
			t.Fatalf("timestamp changes collide at %s%%", match[1])
		}
		seen[match[1]] = true
	}
	if len(seen) != 4 {
		t.Fatalf("SVG has %d timestamp keyframes, want 4", len(seen))
	}
}

func TestEvaluateCaptureFailure(t *testing.T) {
	browser := os.Getenv("VHS_BROWSER_PATH")
	if browser == "" {
		t.Skip("set VHS_BROWSER_PATH to run browser integration")
	}
	if _, err := exec.LookPath("ttyd"); err != nil {
		t.Skip("ttyd is unavailable")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("blocks frame directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	errs := Evaluate(t.Context(), `Output capture-error.gif
Set Shell "bash"
Set TypingSpeed 100ms
Type "echo active-command"
Enter
Type "SHOULD_NOT_RUN"
Enter
`, &output, WithBrowserPath(browser), func(v *VHS) {
		originalInput := v.Options.Video.Input
		t.Cleanup(func() { _ = os.RemoveAll(originalInput) })
		// Setup cannot create a frame directory inside this ordinary file.
		v.Options.Video.Input = filepath.Join(blocker, "frames")
	})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "failed to write frame") {
		t.Fatalf("capture failure = %v, want original frame write error", errs)
	}
	if strings.Contains(output.String(), "SHOULD_NOT_RUN") {
		t.Fatal("Evaluate continued to the next command after capture failure")
	}
}
