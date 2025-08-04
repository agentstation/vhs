package main

import (
	"crypto/md5"
	"fmt"
	"html"
	"strings"
)

// SVGFrame represents a single frame in the SVG animation
type SVGFrame struct {
	Lines         []string
	CursorX       int
	CursorY       int
	CursorPixelX  float64
	CursorPixelY  float64
	Timestamp     float64
	CharWidth     float64
	CharHeight    float64
	LetterSpacing float64
	CharPositions []CharPosition // Exact positions from xterm.js
}

// CharPosition represents exact character position from xterm.js
type CharPosition struct {
	Char string
	X    float64
}

// SVGOptions contains options for SVG generation
type SVGOptions struct {
	Width      int
	Height     int
	FontSize   int
	FontFamily string
	Theme      Theme
	Frames     []SVGFrame
	Duration   float64
	Style      *StyleOptions // Include all style options
}

// TerminalState represents a unique terminal state for deduplication
type TerminalState struct {
	Lines        []string
	CursorX      int
	CursorY      int
	CursorPixelX float64
	CursorPixelY float64
	Hash         string
}

// KeyframeStop represents a point in the animation timeline
type KeyframeStop struct {
	Percentage float64
	StateIndex int
}

// TextSymbol represents a reusable text element
type TextSymbol struct {
	ID      string
	Content string
	Class   string
}

// SVGGenerator handles the generation of optimized animated SVG files
type SVGGenerator struct {
	options       SVGOptions
	charWidth     float64
	charHeight    float64
	fontSize      float64
	states        []TerminalState   // Unique terminal states
	stateMap      map[string]int    // Hash -> state index
	timeline      []KeyframeStop    // Animation timeline
	frameSpacing  float64           // Spacing between frames in SVG units
	textSymbols   map[string]string // Content hash -> symbol ID
	symbolCounter int               // Counter for generating symbol IDs
}

// NewSVGGenerator creates a new SVG generator
func NewSVGGenerator(opts SVGOptions) *SVGGenerator {
	// Get character dimensions from the first frame if available
	charWidth := float64(opts.FontSize) * 0.55 // fallback
	charHeight := float64(opts.FontSize) * 1.2 // fallback

	if len(opts.Frames) > 0 && opts.Frames[0].CharWidth > 0 {
		// Use actual dimensions from xterm.js
		charWidth = opts.Frames[0].CharWidth
		charHeight = opts.Frames[0].CharHeight
	}

	return &SVGGenerator{
		options:      opts,
		charWidth:    charWidth,
		charHeight:   charHeight,
		stateMap:     make(map[string]int),
		textSymbols:  make(map[string]string),
		frameSpacing: 100.0, // 100 units between frames
	}
}

// Generate creates the complete SVG animation
func (g *SVGGenerator) Generate() string {
	// Use style options for dimensions
	style := g.options.Style
	if style == nil {
		style = DefaultStyleOptions()
	}

	// Process frames to extract unique states
	g.processFrames()

	// Calculate fontSize early so it's available for symbol generation
	g.fontSize = float64(g.options.FontSize)
	if g.fontSize <= 0 {
		g.fontSize = 20
	}

	var sb strings.Builder

	// Calculate total dimensions including margins
	totalWidth := style.Width
	totalHeight := style.Height
	if style.Margin > 0 {
		totalWidth += style.Margin * 2
		totalHeight += style.Margin * 2
	}

	// SVG root element
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d">`,
		totalWidth, totalHeight))
	sb.WriteString("\n")

	// Add margin group if needed
	if style.Margin > 0 {
		marginColor := style.MarginFill
		if marginColor == "" {
			marginColor = "#000000"
		}
		sb.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="%s"/>`,
			totalWidth, totalHeight, marginColor))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf(`<g transform="translate(%d,%d)">`, style.Margin, style.Margin))
		sb.WriteString("\n")
	}

	// Terminal window
	sb.WriteString(g.generateTerminalWindow())

	// Calculate inner terminal area
	barHeight := 0
	if style.WindowBar != "" {
		barHeight = style.WindowBarSize
	}

	padding := style.Padding
	innerX := padding
	innerY := barHeight + padding
	innerWidth := style.Width - (padding * 2)
	innerHeight := style.Height - barHeight - (padding * 2)

	// Inner terminal SVG with viewBox for animation
	viewBoxWidth := float64(len(g.states)) * g.frameSpacing
	// Calculate viewBox height based on actual content scale
	contentScale := g.frameSpacing / float64(innerWidth)
	viewBoxHeight := float64(innerHeight) * contentScale

	// Add overflow hidden for clipping
	sb.WriteString(fmt.Sprintf(`<svg x="%d" y="%d" width="%d" height="%d" viewBox="0 0 %.1f %.1f" overflow="hidden">`,
		innerX, innerY, innerWidth, innerHeight, viewBoxWidth, viewBoxHeight))
	sb.WriteString("\n")

	// Add styles including CSS animation
	sb.WriteString(g.generateStyles())

	// Add defs section for reusable elements
	sb.WriteString("<defs>\n")
	sb.WriteString(g.generateCommonSymbols())
	sb.WriteString("</defs>\n")

	// Animation group
	sb.WriteString(`<g class="animation-container">`)
	sb.WriteString("\n")

	// Generate all unique states
	for i, state := range g.states {
		sb.WriteString(g.generateState(i, &state))
	}

	sb.WriteString("</g>\n")   // Close animation container
	sb.WriteString("</svg>\n") // Close inner SVG

	// Close margin group if opened
	if style.Margin > 0 {
		sb.WriteString("</g>\n")
	}

	sb.WriteString("</svg>\n")

	return sb.String()
}

// processFrames deduplicates frames and builds timeline
func (g *SVGGenerator) processFrames() {
	// First pass: collect all unique states
	for i, frame := range g.options.Frames {
		// Create state from frame
		state := TerminalState{
			Lines:        frame.Lines,
			CursorX:      frame.CursorX,
			CursorY:      frame.CursorY,
			CursorPixelX: frame.CursorPixelX,
			CursorPixelY: frame.CursorPixelY,
		}

		// Generate hash for deduplication
		hash := g.hashState(&state)
		state.Hash = hash

		// Check if we've seen this state before
		if idx, exists := g.stateMap[hash]; exists {
			// Reuse existing state
			g.timeline = append(g.timeline, KeyframeStop{
				Percentage: float64(i) / float64(len(g.options.Frames)-1) * 100,
				StateIndex: idx,
			})
		} else {
			// New unique state
			idx := len(g.states)
			g.states = append(g.states, state)
			g.stateMap[hash] = idx
			g.timeline = append(g.timeline, KeyframeStop{
				Percentage: float64(i) / float64(len(g.options.Frames)-1) * 100,
				StateIndex: idx,
			})
		}
	}

	// Second pass: optimize consecutive states with similar content
	g.optimizeIncrementalStates()
}

// optimizeIncrementalStates looks for states that differ only by small increments
func (g *SVGGenerator) optimizeIncrementalStates() {
	// This is a placeholder for future incremental optimization
	// For now, we'll keep the existing deduplication
}

// hashState generates a hash for a terminal state
func (g *SVGGenerator) hashState(state *TerminalState) string {
	h := md5.New()
	for _, line := range state.Lines {
		// Normalize line by trimming trailing spaces for better deduplication
		normalizedLine := strings.TrimRight(line, " ")
		h.Write([]byte(normalizedLine))
		h.Write([]byte("\n"))
	}
	// Include cursor position in hash for accuracy
	h.Write([]byte(fmt.Sprintf("%d,%d,%.2f,%.2f",
		state.CursorX, state.CursorY, state.CursorPixelX, state.CursorPixelY)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// generateStyles creates the CSS styles and animations
func (g *SVGGenerator) generateStyles() string {
	var sb strings.Builder

	sb.WriteString("<style>\n")

	// Generate keyframes
	sb.WriteString("@keyframes slide {\n")

	// Build optimized keyframes - only include frames where state changes
	lastStateIndex := -1
	lastPercentage := -1.0
	for _, stop := range g.timeline {
		// Only add keyframe if state changed AND percentage is different (avoid duplicate percentages)
		if stop.StateIndex != lastStateIndex && stop.Percentage != lastPercentage {
			offset := -float64(stop.StateIndex) * g.frameSpacing
			sb.WriteString(fmt.Sprintf("  %.2f%% { transform: translateX(%.1fpx); }\n",
				stop.Percentage, offset))
			lastStateIndex = stop.StateIndex
			lastPercentage = stop.Percentage
		}
	}

	sb.WriteString("}\n\n")

	// Animation container style
	sb.WriteString(".animation-container {\n")
	sb.WriteString(fmt.Sprintf("  animation: slide %.2fs steps(1, end) infinite;\n", g.options.Duration))
	sb.WriteString("}\n\n")

	// Terminal styles
	theme := g.options.Theme

	// Text styles using classes for deduplication (use short class names for size)
	sb.WriteString(fmt.Sprintf(".f { fill: %s; font-family: %s, monospace; font-size: %.2fpx; }\n",
		theme.Foreground, g.options.FontFamily, g.fontSize))

	// Add common baseline classes to reduce y attribute repetition
	scale := g.frameSpacing / float64(g.options.Width-40)
	for i := 0; i < 30; i++ { // Common lines
		baseline := float64(i)*g.charHeight*scale + g.charHeight*scale*0.8
		sb.WriteString(fmt.Sprintf(".y%d { y: %.3f; }\n", i, baseline))
	}

	// Add styles for common colors to enable better compression
	colorClasses := map[string]string{
		"black":   theme.Black,
		"red":     theme.Red,
		"green":   theme.Green,
		"yellow":  theme.Yellow,
		"blue":    theme.Blue,
		"magenta": theme.Magenta,
		"cyan":    theme.Cyan,
		"white":   theme.White,
	}

	for name, color := range colorClasses {
		sb.WriteString(fmt.Sprintf(".%s { fill: %s; }\n", name, color))
	}

	// Cursor animation
	sb.WriteString("@keyframes blink { 0%, 49% { opacity: 1; } 50%, 100% { opacity: 0; } }\n")
	sb.WriteString(fmt.Sprintf(".cursor { fill: %s; }\n", theme.Cursor))

	sb.WriteString("</style>\n")

	return sb.String()
}

// generateCommonSymbols creates reusable symbol definitions for common elements
func (g *SVGGenerator) generateCommonSymbols() string {
	var sb strings.Builder

	// Common prompt symbol (without x/y to allow positioning via use element)
	sb.WriteString(`<symbol id="prompt"><text class="f">&gt;</text></symbol>`)
	sb.WriteString("\n")

	// Common line starts to reduce repetition
	sb.WriteString(`<symbol id="prompt-echo"><text class="f">&gt; echo</text></symbol>`)
	sb.WriteString("\n")

	// Cursor symbol
	scale := g.frameSpacing / float64(g.options.Width-40)
	sb.WriteString(fmt.Sprintf(`<symbol id="cursor-sym"><rect class="cursor" width="%.3f" height="%.3f" style="animation: blink 1s infinite"/></symbol>`,
		g.charWidth*scale, g.charHeight*scale))
	sb.WriteString("\n")

	return sb.String()
}

// generateState creates a group for a single terminal state
func (g *SVGGenerator) generateState(index int, state *TerminalState) string {
	var sb strings.Builder

	// Position this state in the animation sequence
	xOffset := float64(index) * g.frameSpacing

	sb.WriteString(fmt.Sprintf(`<g transform="translate(%.1f, 0)">`, xOffset))
	sb.WriteString("\n")

	// Scale for viewBox coordinates
	style := g.options.Style
	if style == nil {
		style = DefaultStyleOptions()
	}
	innerWidth := style.Width - (style.Padding * 2)
	scale := g.frameSpacing / float64(innerWidth)

	// Render lines with optimization
	for y, line := range state.Lines {
		if line == "" {
			continue
		}

		// Trim trailing spaces for optimization
		line = strings.TrimRight(line, " ")
		if line == "" {
			continue
		}

		// Ultra-optimized rendering: use tspan for efficient text grouping
		// First, try to render the entire line if possible
		if strings.TrimSpace(line) != "" {
			// Check if we can render the whole line as one element
			leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
			trimmedLine := strings.TrimSpace(line)

			if leadingSpaces == 0 && trimmedLine == line {
				// No leading spaces and no trailing spaces - render as single element
				// Check if it's just a prompt character
				if line == ">" {
					sb.WriteString(fmt.Sprintf(`<use href="#prompt" x="0" class="y%d"/>`, y))
				} else {
					sb.WriteString(fmt.Sprintf(`<text x="0" class="f y%d">%s</text>`,
						y, html.EscapeString(line)))
				}
				sb.WriteString("\n")
			} else {
				// Use text with tspan for complex lines
				sb.WriteString(fmt.Sprintf(`<text class="f y%d">`, y))

				x := 0
				for x < len(line) {
					// Find next non-space segment
					for x < len(line) && line[x] == ' ' {
						x++
					}

					if x >= len(line) {
						break
					}

					// Find run of non-space characters
					textStart := x
					for x < len(line) && line[x] != ' ' {
						x++
					}

					// Render the text segment
					if x > textStart {
						text := line[textStart:x]
						charX := float64(textStart) * g.charWidth * scale
						sb.WriteString(fmt.Sprintf(`<tspan x="%.3f">%s</tspan>`,
							charX, html.EscapeString(text)))
					}
				}

				sb.WriteString("</text>\n")
			}
		}
	}

	// Add cursor using symbol
	if state.CursorY >= 0 && state.CursorY < len(state.Lines) {
		cursorX := state.CursorPixelX * scale
		cursorY := state.CursorPixelY * scale

		sb.WriteString(fmt.Sprintf(`<use href="#cursor-sym" x="%.3f" y="%.3f"/>`,
			cursorX, cursorY))
		sb.WriteString("\n")
	}

	sb.WriteString("</g>\n")

	return sb.String()
}

// generateTerminalWindow creates the terminal window chrome
func (g *SVGGenerator) generateTerminalWindow() string {
	var sb strings.Builder

	style := g.options.Style
	if style == nil {
		style = DefaultStyleOptions()
	}

	// Window background with configurable rounded corners
	borderRadius := style.BorderRadius
	if borderRadius < 0 {
		borderRadius = 0
	}

	// Use background color from style or theme
	bgColor := style.BackgroundColor
	if bgColor == "" {
		bgColor = "#1e1e1e"
	}

	sb.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" rx="%d" fill="%s"/>`,
		g.options.Width, g.options.Height, borderRadius, bgColor))
	sb.WriteString("\n")

	// Window bar if enabled
	if style.WindowBar != "" {
		sb.WriteString(g.generateWindowBar())
	}

	return sb.String()
}

// generateWindowBar creates the window bar based on style
func (g *SVGGenerator) generateWindowBar() string {
	var sb strings.Builder

	style := g.options.Style
	if style == nil {
		style = DefaultStyleOptions()
	}

	barSize := style.WindowBarSize
	barColor := style.WindowBarColor
	if barColor == "" {
		barColor = "#2d2d2d"
	}

	// Bar background
	borderRadius := style.BorderRadius
	if borderRadius < 0 {
		borderRadius = 0
	}

	sb.WriteString(fmt.Sprintf(`<g id="window-bar">`))
	sb.WriteString("\n")

	// Bar background with rounded top corners
	sb.WriteString(fmt.Sprintf(`<path d="M %d,0 L %d,0 Q %d,0 %d,%d L %d,%d L 0,%d L 0,%d Q 0,0 %d,0 Z" fill="%s"/>`,
		borderRadius, g.options.Width-borderRadius, g.options.Width, g.options.Width, borderRadius,
		g.options.Width, barSize, barSize, borderRadius, borderRadius, barColor))
	sb.WriteString("\n")

	// Window controls based on style
	switch style.WindowBar {
	case "darwin", "":
		// macOS-style window controls
		colors := []string{"#ff5f58", "#ffbd2e", "#18c132"}
		for i, color := range colors {
			x := 20 + i*20
			sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="6" fill="%s"/>`, x, barSize/2, color))
			sb.WriteString("\n")
		}
	case "windows":
		// Windows-style controls (minimize, maximize, close)
		x := g.options.Width - 20
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="14" height="2" fill="#999"/>`, x-50, barSize/2-1))
		sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="12" height="12" fill="none" stroke="#999" stroke-width="2"/>`, x-30, barSize/2-6))
		sb.WriteString(fmt.Sprintf(`<path d="M %d,%d L %d,%d M %d,%d L %d,%d" stroke="#999" stroke-width="2"/>`,
			x-14, barSize/2-6, x-2, barSize/2+6, x-2, barSize/2-6, x-14, barSize/2+6))
		sb.WriteString("\n")
	case "filled":
		// Filled circles
		for i := 0; i < 3; i++ {
			x := 20 + i*20
			sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="6" fill="#888"/>`, x, barSize/2))
		}
		sb.WriteString("\n")
	case "outline":
		// Outlined circles
		for i := 0; i < 3; i++ {
			x := 20 + i*20
			sb.WriteString(fmt.Sprintf(`<circle cx="%d" cy="%d" r="6" fill="none" stroke="#888" stroke-width="1"/>`, x, barSize/2))
		}
		sb.WriteString("\n")
	}

	// Title text
	sb.WriteString(fmt.Sprintf(`<text x="%d" y="%d" text-anchor="middle" font-family="%s,monospace" font-size="13" fill="#cccccc">Terminal</text>`,
		g.options.Width/2, barSize/2+4, g.options.FontFamily))
	sb.WriteString("\n")

	sb.WriteString("</g>\n")

	return sb.String()
}
