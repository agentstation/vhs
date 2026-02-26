// Package vhs video.go spawns the ffmpeg process to convert the frames,
// collected by go-rod's  screenshots into the input folder, to a GIF, WebM,
// MP4.
//
// MakeGIF takes several options to modify the behaviour of the ffmpeg process,
// which can be configured through the Set command.
//
// Set MaxColors 256
package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	textFrameFormat   = "frame-text-%05d.png"
	cursorFrameFormat = "frame-cursor-%05d.png"
)

const (
	mp4  = ".mp4"
	webm = ".webm"
	gif  = ".gif"
	svg  = ".svg"
)

// randomDir returns a random temporary directory to be used for storing frames
// from screenshots of the terminal.
func randomDir() string {
	tmp, err := os.MkdirTemp(os.TempDir(), "vhs")
	if err != nil {
		log.Printf("Error creating temporary directory: %v", err)
	}
	return tmp
}

// VideoOutputs is a mapping from file type to file path for all video outputs
// of VHS.
type VideoOutputs struct {
	GIF    string
	WebM   string
	MP4    string
	SVG    string
	Frames string
}

// VideoOptions is the set of options for converting frames to a GIF.
type VideoOptions struct {
	Framerate     int
	PlaybackSpeed float64
	Input         string
	MaxColors     int
	Output        VideoOutputs
	StartingFrame int
	Style         *StyleOptions
}

const (
	defaultFramerate     = 50
	defaultStartingFrame = 1
)

// DefaultVideoOptions is the set of default options for converting frames
// to a GIF, which are used if they are not overridden.
func DefaultVideoOptions() VideoOptions {
	return VideoOptions{
		Framerate:     defaultFramerate,
		Input:         randomDir(),
		MaxColors:     defaultMaxColors,
		Output:        VideoOutputs{GIF: "", WebM: "", MP4: "", SVG: "", Frames: ""},
		PlaybackSpeed: defaultPlaybackSpeed,
		StartingFrame: defaultStartingFrame,
	}
}

func marginFillIsColor(marginFill string) bool {
	return strings.HasPrefix(marginFill, "#")
}

// makeMedia takes a list of images (as frames) and converts them to a GIF/WebM/MP4.
func makeMedia(opts VideoOptions, targetFile string) *exec.Cmd {
	if targetFile == "" {
		return nil
	}

	log.Println(GrayStyle.Render("Creating " + targetFile + "..."))
	ensureDir(targetFile)

	//nolint:gosec,noctx
	return exec.Command(
		"ffmpeg",
		buildFFopts(opts, targetFile)...,
	)
}

// ensureDir ensures that the file path of the output can be created by
// creating all the necessary nested folders.
func ensureDir(output string) {
	err := os.MkdirAll(filepath.Dir(output), 0o750)
	if err != nil {
		fmt.Println(ErrorStyle.Render("Unable to create output directory: "), output)
	}
}

// buildFFopts assembles an ffmpeg command from some VideoOptions.
func buildFFopts(opts VideoOptions, targetFile string) []string {
	var args []string
	streamCounter := 2

	streamBuilder := NewStreamBuilder(streamCounter, opts.Input, opts.Style)

	// Input frame options, used no matter what
	// Stream 0: text frames
	// Stream 1: cursor frames
	streamBuilder.args = append(streamBuilder.args,
		"-y",
		"-r", fmt.Sprint(opts.Framerate),
		"-start_number", fmt.Sprint(opts.StartingFrame),
		"-i", filepath.Join(opts.Input, textFrameFormat),
		"-r", fmt.Sprint(opts.Framerate),
		"-start_number", fmt.Sprint(opts.StartingFrame),
		"-i", filepath.Join(opts.Input, cursorFrameFormat),
	)

	streamBuilder = streamBuilder.
		WithMargin().
		WithBar().
		WithCorner()

	filterBuilder := NewVideoFilterBuilder(&opts).
		WithWindowBar(streamBuilder.barStream).
		WithBorderRadius(streamBuilder.cornerStream).
		WithMarginFill(streamBuilder.marginStream)

	// Format-specific options
	switch filepath.Ext(targetFile) {
	case gif:
		filterBuilder = filterBuilder.WithGIF()
	case webm:
		streamBuilder = streamBuilder.WithWebm()
	case mp4:
		streamBuilder = streamBuilder.WithMP4()
	}

	args = append(args, streamBuilder.Build()...)
	args = append(args, filterBuilder.Build()...)
	args = append(args, targetFile)

	return args
}

// MakeGIF takes a list of images (as frames) and converts them to a GIF.
func MakeGIF(opts VideoOptions) *exec.Cmd {
	return makeMedia(opts, opts.Output.GIF)
}

// MakeWebM takes a list of images (as frames) and converts them to a WebM.
func MakeWebM(opts VideoOptions) *exec.Cmd {
	return makeMedia(opts, opts.Output.WebM)
}

// MakeMP4 takes a list of images (as frames) and converts them to an MP4.
func MakeMP4(opts VideoOptions) *exec.Cmd {
	return makeMedia(opts, opts.Output.MP4)
}

// MakeSVG generates an animated SVG from captured frames.
func MakeSVG(v *VHS) error {
	if v.Options.Video.Output.SVG == "" || len(v.svgFrames) == 0 {
		if v.Options.Video.Output.SVG == "" {
			log.Println("No SVG output path specified")
		} else {
			log.Printf("No SVG frames captured (0 frames)")
		}
		return nil
	}

	log.Println(GrayStyle.Render("Creating " + v.Options.Video.Output.SVG + "..."))
	ensureDir(v.Options.Video.Output.SVG)

	// Calculate total duration based on frame count and framerate
	duration := float64(len(v.svgFrames)) / float64(v.Options.Video.Framerate)

	// Try to embed the font for portable SVG rendering.
	// Uses fc-match to find the font file, then base64-encodes it.
	// If pyftsubset is available, subset the font to only the glyphs used.
	fontData, fontMIME := resolveFont(v.Options.FontFamily, v.svgFrames)

	// Create SVG config
	svgOpts := SVGConfig{
		Width:         v.Options.Video.Style.Width,
		Height:        v.Options.Video.Style.Height,
		FontSize:      v.Options.FontSize,
		FontFamily:    v.Options.FontFamily,
		Theme:         v.Options.Theme,
		Frames:        v.svgFrames,
		Duration:      duration,
		Style:         v.Options.Video.Style,
		LineHeight:    v.Options.LineHeight,
		CursorBlink:   v.Options.CursorBlink,
		PlaybackSpeed: v.Options.Video.PlaybackSpeed,
		LoopOffset:    v.Options.LoopOffset,
		OptimizeSize:  v.Options.SVG.OptimizeSize,
		Debug:         v.Options.DebugConsole,
		FontData:      fontData,
		FontMIME:      fontMIME,
	}

	// Generate SVG
	generator := NewSVGGenerator(svgOpts)
	svgContent := generator.Generate()

	// Write to file
	if err := os.WriteFile(v.Options.Video.Output.SVG, []byte(svgContent), 0o600); err != nil {
		return fmt.Errorf("failed to write SVG file: %w", err)
	}

	return nil
}

// resolveFont finds the font file for the given family using fc-match,
// subsets it to only the glyphs used in the SVG frames (if pyftsubset
// is available), and returns the base64-encoded data with its MIME type.
// Returns empty strings if the font cannot be resolved.
func resolveFont(fontFamily string, frames []SVGFrame) (string, string) {
	// Only embed a single, explicitly-set font family. The default font stack
	// is a comma-separated fallback list for browser rendering — embedding the
	// first match would produce a @font-face name that doesn't match the CSS
	// font-family property on text elements.
	if fontFamily == "" || fontFamily == "monospace" || strings.Contains(fontFamily, ",") {
		return "", ""
	}

	// Use fc-match to find the font file path
	out, err := exec.Command("fc-match", fontFamily, "--format=%{file}").Output()
	if err != nil {
		log.Printf("fc-match failed for %q: %v", fontFamily, err)
		return "", ""
	}

	fontPath := strings.TrimSpace(string(out))
	if fontPath == "" {
		return "", ""
	}
	if _, err := os.Stat(fontPath); err != nil {
		log.Printf("Font file not found: %s", fontPath)
		return "", ""
	}

	// Try subsetting with pyftsubset for smaller output
	if data, ok := subsetFont(fontPath, frames); ok {
		encoded := base64.StdEncoding.EncodeToString(data)
		log.Printf("Embedding subset font (%d KB, woff2)", len(data)/1024)
		return encoded, "font/woff2"
	}

	// Fallback: embed the full font file
	data, err := os.ReadFile(fontPath)
	if err != nil {
		log.Printf("Failed to read font file %s: %v", fontPath, err)
		return "", ""
	}

	mime := "font/truetype"
	switch strings.ToLower(filepath.Ext(fontPath)) {
	case ".woff2":
		mime = "font/woff2"
	case ".woff":
		mime = "font/woff"
	case ".otf":
		mime = "font/opentype"
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	log.Printf("Embedding full font %s (%s, %d KB)", fontPath, mime, len(data)/1024)

	return encoded, mime
}

// subsetFont uses pyftsubset to create a woff2 subset of the font
// containing only the glyphs used in the given SVG frames.
// Returns the woff2 data and true on success, or nil and false on failure.
func subsetFont(fontPath string, frames []SVGFrame) ([]byte, bool) {
	if _, err := exec.LookPath("pyftsubset"); err != nil {
		return nil, false
	}

	// Collect unique codepoints from all frame text
	codepoints := make(map[rune]struct{})
	for _, frame := range frames {
		for _, line := range frame.Lines {
			for _, r := range line {
				codepoints[r] = struct{}{}
			}
		}
		if frame.CursorChar != "" {
			for _, r := range frame.CursorChar {
				codepoints[r] = struct{}{}
			}
		}
	}

	if len(codepoints) == 0 {
		return nil, false
	}

	// Build Unicode range string for pyftsubset (e.g. "U+0041,U+0042")
	unicodes := make([]string, 0, len(codepoints))
	for r := range codepoints {
		unicodes = append(unicodes, fmt.Sprintf("U+%04X", r))
	}

	// Create temp file for output
	tmpFile, err := os.CreateTemp("", "vhs-font-*.woff2")
	if err != nil {
		return nil, false
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Run pyftsubset
	//nolint:gosec
	cmd := exec.Command("pyftsubset", fontPath,
		"--unicodes="+strings.Join(unicodes, ","),
		"--flavor=woff2",
		"--output-file="+tmpPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("pyftsubset failed: %v: %s", err, out)
		return nil, false
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, false
	}

	log.Printf("Font subset: %d glyphs, %d KB woff2 (from %s)",
		len(codepoints), len(data)/1024, fontPath)

	return data, true
}
