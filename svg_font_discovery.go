package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const titleFontFamily = "VHS Title Font"

func namedSVGFont(family string) bool {
	if family == "" || strings.Contains(family, ",") {
		return false
	}
	switch strings.ToLower(family) {
	case "monospace", "ui-monospace", "serif", "sans-serif", "cursive", "fantasy", "system-ui", "ui-serif", "ui-sans-serif", "ui-rounded", "math", "fangsong", "inherit", "initial", "unset", "revert", "revert-layer":
		return false
	}
	return true
}

func discoverSVGFont(ctx context.Context, family string) (svgFont, error) {
	family = strings.Trim(strings.TrimSpace(family), "\"'")
	if !namedSVGFont(family) {
		return svgFont{}, nil
	}
	tool, err := exec.LookPath("fc-match")
	if err != nil {
		fontDiagnostic(family, "fc-match is unavailable; install Fontconfig or use --svg-font-file for portable SVG output")
		return svgFont{}, nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, tool, "--format=%{file}\n%{index}\n%{family}\n", "--", family).Output()
	if err != nil {
		if ctx.Err() != nil {
			return svgFont{}, fmt.Errorf("SVG font discovery canceled: %w", ctx.Err())
		}
		fontDiagnostic(family, fmt.Sprintf("fc-match failed: %v; use --svg-font-file for portable SVG output", err))
		return svgFont{}, nil
	}
	fields := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(fields) != 3 || fields[0] == "" {
		return svgFont{}, fmt.Errorf("fc-match returned invalid SVG font metadata for %q", family)
	}
	matched := false
	for alias := range strings.SplitSeq(fields[2], ",") {
		matched = matched || strings.EqualFold(strings.TrimSpace(alias), family)
	}
	if !matched {
		fontDiagnostic(family, fmt.Sprintf("not installed (Fontconfig matched %q); use --svg-font-file for portable SVG output", fields[2]))
		return svgFont{}, nil
	}
	index, err := strconv.Atoi(fields[1])
	if err != nil || index < 0 || index > 0xffff {
		return svgFont{}, fmt.Errorf("fc-match returned an unsupported SVG font face index %q", fields[1])
	}
	font, err := readDiscoveredSVGFont(fields[0], index)
	if err != nil {
		return svgFont{}, fmt.Errorf("failed to resolve SVG font %q: %w", family, err)
	}
	font.auto = true
	return font, nil
}

func readDiscoveredSVGFont(path string, index int) (svgFont, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return svgFont{}, fmt.Errorf("failed to read resolved font: %w", err)
	}
	if strings.HasPrefix(string(data), "ttcf") {
		data, err = extractSVGFontFace(data, index)
		if err != nil {
			return svgFont{}, err
		}
		mime, format := fontMIMETTF, fontFormatTTF
		if strings.HasPrefix(string(data), "OTTO") {
			mime, format = fontMIMEOTF, fontFormatOTF
		}
		return svgFont{data: base64.StdEncoding.EncodeToString(data), mime: mime, format: format}, nil
	}
	if index != 0 {
		return svgFont{}, errors.New("resolved font face index is not zero for a single-face file")
	}
	return readSVGFont(path)
}
