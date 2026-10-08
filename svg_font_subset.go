package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type svgFonts struct {
	terminal svgFont
	title    svgFont
}

func (vhs *VHS) prepareSVGFonts(ctx context.Context) error {
	if vhs.Options.Video.Output.SVG == "" || vhs.svgOutputFonts != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("SVG font preparation canceled: %w", err)
	}
	main, title, err := vhs.outputSVGFonts(ctx)
	if err != nil {
		return err
	}
	vhs.svgOutputFonts = &svgFonts{terminal: main, title: title}
	return nil
}

func (vhs *VHS) outputSVGFonts(ctx context.Context) (svgFont, svgFont, error) {
	main, title := vhs.svgFont, vhs.svgTitleFont
	if !vhs.Options.SVG.OptimizeSize {
		return main, title, nil
	}
	var text strings.Builder
	for _, frame := range vhs.svgFrames {
		for _, line := range frame.Lines {
			text.WriteString(line)
		}
		text.WriteString(frame.CursorChar)
	}
	style := vhs.Options.Video.Style
	if style != nil && style.WindowBar != "" && style.WindowBarFontFamily == "" {
		text.WriteString(style.WindowBarTitle)
	}
	var err error
	main, err = subsetSVGFont(ctx, main, text.String())
	if err != nil {
		return main, title, err
	}
	main, err = vhs.validateSVGSubset(ctx, vhs.svgFont, main)
	if err != nil {
		return main, title, err
	}
	if style != nil {
		title, err = subsetSVGFont(ctx, title, style.WindowBarTitle)
		if err == nil {
			title, err = vhs.validateSVGSubset(ctx, vhs.svgTitleFont, title)
		}
	}
	return main, title, err
}

func (vhs *VHS) validateSVGSubset(ctx context.Context, original, subset svgFont) (svgFont, error) {
	if vhs.Page == nil || original.data == subset.data {
		return subset, nil
	}
	url := "data:" + subset.mime + ";base64," + subset.data
	_, err := vhs.Page.Context(ctx).Eval(`async url => {
		const font = new FontFace('VHS Subset Validation', 'url("' + url + '")');
		await font.load();
	}`, url)
	if err != nil {
		if ctx.Err() != nil {
			return original, fmt.Errorf("SVG font validation canceled: %w", ctx.Err())
		}
		fontDiagnostic(captureFontFamily, "subset font failed browser validation; embedding full selected font")
		return original, nil
	}
	return subset, nil
}

func subsetSVGFont(ctx context.Context, font svgFont, text string) (svgFont, error) {
	if !font.auto || font.data == "" || text == "" {
		return font, nil
	}
	tool, err := exec.LookPath("pyftsubset")
	if err != nil {
		fontDiagnostic(captureFontFamily, "pyftsubset is unavailable; embedding the full selected font")
		return font, nil
	}
	dir, err := os.MkdirTemp("", "vhs-font-*")
	if err != nil {
		return font, fmt.Errorf("failed to create SVG font subset directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	data, err := base64.StdEncoding.DecodeString(font.data)
	if err != nil {
		return font, fmt.Errorf("failed to decode selected SVG font: %w", err)
	}
	input, output := filepath.Join(dir, "selected-font"), filepath.Join(dir, "subset.woff2")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		return font, fmt.Errorf("failed to write selected SVG font: %w", err)
	}
	codepoints := make(map[rune]bool)
	for _, r := range text {
		codepoints[r] = true
	}
	unicodes := make([]string, 0, len(codepoints))
	for r := range codepoints {
		unicodes = append(unicodes, fmt.Sprintf("U+%04X", r))
	}
	slices.Sort(unicodes)
	unicodeFile := filepath.Join(dir, "unicodes")
	if err := os.WriteFile(unicodeFile, []byte(strings.Join(unicodes, ",")), 0o600); err != nil {
		return font, fmt.Errorf("failed to write SVG font glyph selection: %w", err)
	}
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, tool, input, "--unicodes-file="+unicodeFile, "--flavor=woff2", "--output-file="+output, "--layout-features=*").CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return font, fmt.Errorf("SVG font subsetting canceled: %w", ctx.Err())
		}
		fontDiagnostic(captureFontFamily, fmt.Sprintf("pyftsubset failed; embedding full selected font: %v: %s", err, out))
		return font, nil
	}
	data, err = os.ReadFile(output)
	if err != nil || !validWOFF2Header(data) {
		fontDiagnostic(captureFontFamily, "pyftsubset produced no valid WOFF2 output; embedding full selected font")
		return font, nil
	}
	if len(data) >= base64.StdEncoding.DecodedLen(len(font.data))-2 {
		return font, nil
	}
	font.data, font.mime, font.format = base64.StdEncoding.EncodeToString(data), "font/woff2", fontFormatWOFF2
	return font, nil
}

func validWOFF2Header(data []byte) bool {
	return len(data) >= 48 && string(data[:4]) == "wOF2" &&
		uint64(binary.BigEndian.Uint32(data[8:12])) == uint64(len(data)) &&
		binary.BigEndian.Uint16(data[12:14]) > 0 &&
		binary.BigEndian.Uint32(data[16:20]) > 0 &&
		binary.BigEndian.Uint32(data[20:24]) > 0 &&
		uint64(binary.BigEndian.Uint32(data[20:24])) <= uint64(len(data)-48)
}
