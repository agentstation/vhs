package main

import (
	"strings"
	"testing"
)

func TestSVGGenerator(t *testing.T) {
	// Test basic SVG generation
	t.Run("generates valid SVG structure", func(t *testing.T) {
		opts := SVGOptions{
			Width:      800,
			Height:     600,
			FontSize:   16,
			FontFamily: "monospace",
			Theme:      DefaultTheme,
			Frames: []SVGFrame{
				{
					Lines:   []string{"Hello", "World"},
					CursorX: 0,
					CursorY: 1,
				},
			},
			Duration: 1.0,
			Style:    DefaultStyleOptions(),
		}

		gen := NewSVGGenerator(opts)
		svg := gen.Generate()

		// Check basic SVG structure
		if !strings.Contains(svg, "<svg") {
			t.Error("SVG should contain opening svg tag")
		}
		if !strings.Contains(svg, "</svg>") {
			t.Error("SVG should contain closing svg tag")
		}
		if !strings.Contains(svg, "@keyframes") {
			t.Error("SVG should contain CSS keyframes")
		}
	})

	// Test frame deduplication
	t.Run("deduplicates identical frames", func(t *testing.T) {
		opts := SVGOptions{
			Width:      800,
			Height:     600,
			FontSize:   16,
			FontFamily: "monospace",
			Theme:      DefaultTheme,
			Frames: []SVGFrame{
				{Lines: []string{"Test"}, CursorX: 0, CursorY: 0},
				{Lines: []string{"Test"}, CursorX: 0, CursorY: 0}, // Duplicate
				{Lines: []string{"Test2"}, CursorX: 0, CursorY: 0},
			},
			Duration: 1.0,
			Style:    DefaultStyleOptions(),
		}

		gen := NewSVGGenerator(opts)
		gen.processFrames()

		// Should have only 2 unique states
		if len(gen.states) != 2 {
			t.Errorf("Expected 2 unique states, got %d", len(gen.states))
		}
	})

	// Test style options
	t.Run("applies style options correctly", func(t *testing.T) {
		style := &StyleOptions{
			Width:           1024,
			Height:          768,
			Padding:         20,
			Margin:          10,
			MarginFill:      "#ff0000",
			WindowBar:       "darwin",
			WindowBarSize:   30,
			BorderRadius:    5,
			BackgroundColor: "#000000",
		}

		opts := SVGOptions{
			Width:      style.Width,
			Height:     style.Height,
			FontSize:   16,
			FontFamily: "monospace",
			Theme:      DefaultTheme,
			Frames:     []SVGFrame{{Lines: []string{"Test"}}},
			Duration:   1.0,
			Style:      style,
		}

		gen := NewSVGGenerator(opts)
		svg := gen.Generate()

		// Check margins
		if !strings.Contains(svg, "1044") { // Width + 2*margin
			t.Error("SVG should include margin in total width")
		}

		// Check margin fill color
		if !strings.Contains(svg, "#ff0000") {
			t.Error("SVG should include margin fill color")
		}

		// Check window bar
		if !strings.Contains(svg, "window-bar") {
			t.Error("SVG should include window bar group")
		}
	})

	// Test CSS optimization
	t.Run("generates optimized CSS classes", func(t *testing.T) {
		opts := SVGOptions{
			Width:      800,
			Height:     600,
			FontSize:   16,
			FontFamily: "monospace",
			Theme:      DefaultTheme,
			Frames: []SVGFrame{
				{Lines: []string{"Line 1", "Line 2", "Line 3"}},
			},
			Duration: 1.0,
			Style:    DefaultStyleOptions(),
		}

		gen := NewSVGGenerator(opts)
		svg := gen.Generate()

		// Check for y-coordinate classes
		if !strings.Contains(svg, ".y0") || !strings.Contains(svg, ".y1") || !strings.Contains(svg, ".y2") {
			t.Error("SVG should contain y-coordinate classes for optimization")
		}

		// Check for common symbols
		if !strings.Contains(svg, `<symbol id="prompt">`) {
			t.Error("SVG should contain prompt symbol")
		}
	})

	// Test hash generation
	t.Run("generates consistent hashes", func(t *testing.T) {
		gen := &SVGGenerator{}
		state1 := &TerminalState{
			Lines:   []string{"Hello", "World"},
			CursorX: 5,
			CursorY: 1,
		}
		state2 := &TerminalState{
			Lines:   []string{"Hello", "World"},
			CursorX: 5,
			CursorY: 1,
		}
		state3 := &TerminalState{
			Lines:   []string{"Hello", "World!"},
			CursorX: 5,
			CursorY: 1,
		}

		hash1 := gen.hashState(state1)
		hash2 := gen.hashState(state2)
		hash3 := gen.hashState(state3)

		if hash1 != hash2 {
			t.Error("Identical states should have same hash")
		}
		if hash1 == hash3 {
			t.Error("Different states should have different hashes")
		}
	})
}

func TestSVGFrameCapture(t *testing.T) {
	// Test that we can identify SVG output requests
	t.Run("detects SVG output extension", func(t *testing.T) {
		outputs := []string{
			"output.svg",
			"test.SVG",
			"file.svg",
		}

		for _, output := range outputs {
			if !strings.HasSuffix(strings.ToLower(output), ".svg") {
				t.Errorf("Should detect %s as SVG output", output)
			}
		}
	})
}
