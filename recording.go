package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// recordingTimeline measures visible time across Hide and Show commands.
type recordingTimeline struct {
	visibleStart time.Time
	elapsed      time.Duration
	generation   int
	stopped      bool
}

func (r *recordingTimeline) resume(now time.Time) {
	if r.stopped || !r.visibleStart.IsZero() {
		return
	}
	r.visibleStart = now
	r.generation++
}

func (r *recordingTimeline) pause(now time.Time) {
	if r.visibleStart.IsZero() {
		return
	}
	r.elapsed += now.Sub(r.visibleStart)
	r.visibleStart = time.Time{}
	r.generation++
}

func (r *recordingTimeline) duration(now time.Time) time.Duration {
	if r.visibleStart.IsZero() {
		return r.elapsed
	}
	return r.elapsed + now.Sub(r.visibleStart)
}

func captureBrowserPath(path string) string {
	if path != "" {
		return path
	}
	return os.Getenv("VHS_BROWSER_PATH")
}

func (o VideoOutputs) needsRaster() bool {
	return o.GIF != "" || o.MP4 != "" || o.WebM != "" || o.Frames != ""
}

// Record captures frames until cancellation or the first capture error.
func (vhs *VHS) Record(ctx context.Context) <-chan error {
	ch := make(chan error, 1)
	vhs.mutex.Lock()
	if vhs.recording {
		vhs.timeline.resume(time.Now())
	}
	vhs.mutex.Unlock()

	// Stop the timeline at cancellation, before browser cleanup or a slow capture.
	stopped := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		vhs.mutex.Lock()
		vhs.timeline.pause(time.Now())
		vhs.timeline.stopped = true
		vhs.mutex.Unlock()
		close(stopped)
	})

	go func() {
		defer close(ch)
		err := vhs.recordFrames(ctx, vhs.captureFrame)
		if !stop() {
			<-stopped
		}
		vhs.mutex.Lock()
		vhs.timeline.pause(time.Now())
		vhs.timeline.stopped = true
		vhs.duration = vhs.timeline.elapsed
		vhs.mutex.Unlock()
		if err != nil {
			ch <- err
		}
	}()
	return ch
}

func (vhs *VHS) recordFrames(ctx context.Context, capture func(float64) error) error {
	framerate := vhs.Options.Video.Framerate
	if framerate <= 0 || framerate > int(time.Second) {
		return fmt.Errorf("invalid capture framerate: %d", framerate)
	}
	ticker := time.NewTicker(time.Second / time.Duration(framerate))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if ctx.Err() != nil {
				return nil
			}
			vhs.mutex.Lock()
			visible := vhs.recording && !vhs.timeline.stopped
			timestamp := vhs.timeline.duration(time.Now()).Seconds()
			vhs.mutex.Unlock()
			if visible {
				if err := capture(timestamp); err != nil {
					return fmt.Errorf("failed to capture frame: %w", err)
				}
			}
		}
	}
}

func (vhs *VHS) captureFrame(_ float64) error {
	vhs.mutex.Lock()
	if !vhs.recording || vhs.timeline.stopped {
		vhs.mutex.Unlock()
		return nil
	}
	timestamp := vhs.timeline.duration(time.Now()).Seconds()
	generation := vhs.timeline.generation
	screenshotPath := vhs.Options.Screenshot.nextScreenshotPath
	vhs.mutex.Unlock()

	var frame *SVGFrame
	if vhs.Options.Video.Output.SVG != "" {
		var err error
		frame, err = CaptureSVGFrame(vhs.Page, timestamp)
		if err != nil {
			return err
		}
		if frame == nil {
			return fmt.Errorf("empty SVG frame")
		}
	}

	captureRaster := vhs.Options.Video.Output.needsRaster() || screenshotPath != ""
	counter := len(vhs.rasterFrames) + 1
	if captureRaster {
		if vhs.CursorCanvas == nil || vhs.TextCanvas == nil {
			return fmt.Errorf("terminal canvas is unavailable")
		}
		const quality = 1.0
		cursor, err := vhs.CursorCanvas.CanvasToImage("image/png", quality)
		if err != nil {
			return fmt.Errorf("failed to capture cursor: %w", err)
		}
		text, err := vhs.TextCanvas.CanvasToImage("image/png", quality)
		if err != nil {
			return fmt.Errorf("failed to capture text: %w", err)
		}
		for name, data := range map[string][]byte{
			fmt.Sprintf(cursorFrameFormat, counter): cursor,
			fmt.Sprintf(textFrameFormat, counter):   text,
		} {
			if err := os.WriteFile(filepath.Join(vhs.Options.Video.Input, name), data, 0o600); err != nil {
				return fmt.Errorf("failed to write frame: %w", err)
			}
		}
	}

	vhs.mutex.Lock()
	defer vhs.mutex.Unlock()
	// Discard captures that overlap a Hide or Show transition.
	if vhs.timeline.generation != generation || !vhs.recording || vhs.timeline.stopped {
		return nil
	}
	if frame != nil {
		vhs.svgFrames = append(vhs.svgFrames, *frame)
	}
	if captureRaster {
		vhs.rasterFrames = append(vhs.rasterFrames, timestamp)
		vhs.totalFrames = counter
		if screenshotPath != "" && vhs.Options.Screenshot.nextScreenshotPath == screenshotPath {
			vhs.Options.Screenshot.makeScreenshot(counter)
		}
	}
	return nil
}

// prepareRasterFrames repeats captured states to preserve elapsed playback time.
func (vhs *VHS) prepareRasterFrames() error {
	if !vhs.Options.Video.Output.needsRaster() || len(vhs.rasterFrames) == 0 {
		return nil
	}
	input := vhs.Options.Video.Input
	captured := input + "-captured"
	if err := os.Rename(input, captured); err != nil {
		return fmt.Errorf("failed to retain captured frames: %w", err)
	}
	defer func() { _ = os.RemoveAll(captured) }()
	if err := os.MkdirAll(input, 0o750); err != nil {
		return fmt.Errorf("failed to create playback frames directory: %w", err)
	}

	framerate := vhs.Options.Video.Framerate
	count := max(1, int(math.Ceil(vhs.duration.Seconds()*float64(framerate))))
	captureIndex := 0
	for i := range count {
		timestamp := float64(i) / float64(framerate)
		for captureIndex+1 < len(vhs.rasterFrames) && vhs.rasterFrames[captureIndex+1] <= timestamp {
			captureIndex++
		}
		for _, format := range []string{textFrameFormat, cursorFrameFormat} {
			from := filepath.Join(captured, fmt.Sprintf(format, captureIndex+1))
			to := filepath.Join(input, fmt.Sprintf(format, i+1))
			if err := os.Link(from, to); err != nil {
				return fmt.Errorf("failed to repeat playback frame: %w", err)
			}
		}
	}
	// Retain original screenshot captures outside the playback sequence.
	if len(vhs.Options.Screenshot.screenshots) > 0 {
		screenshotInput := filepath.Join(input, "screenshots")
		if err := os.MkdirAll(screenshotInput, 0o750); err != nil {
			return fmt.Errorf("failed to create screenshot frames directory: %w", err)
		}
		for _, frame := range vhs.Options.Screenshot.screenshots {
			for _, format := range []string{textFrameFormat, cursorFrameFormat} {
				name := fmt.Sprintf(format, frame)
				to := filepath.Join(screenshotInput, name)
				if _, err := os.Stat(to); err == nil {
					continue
				}
				if err := os.Link(filepath.Join(captured, name), to); err != nil {
					return fmt.Errorf("failed to retain screenshot frame: %w", err)
				}
			}
		}
		vhs.Options.Screenshot.input = screenshotInput
	}

	vhs.totalFrames = count
	return nil
}
