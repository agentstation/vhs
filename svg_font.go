package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const captureFontFamily = "VHS Capture Font"

type svgFont struct {
	data   string
	mime   string
	format string
}

func readSVGFont(path string) (svgFont, error) {
	var font svgFont
	switch strings.ToLower(filepath.Ext(path)) {
	case ".woff2":
		font.mime, font.format = "font/woff2", "woff2"
	case ".woff":
		font.mime, font.format = "font/woff", "woff"
	case ".ttf":
		font.mime, font.format = "font/ttf", "truetype"
	case ".otf":
		font.mime, font.format = "font/otf", "opentype"
	default:
		return font, fmt.Errorf("SVG font file must use .woff2, .woff, .ttf, or .otf")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return font, fmt.Errorf("failed to read SVG font: %w", err)
	}
	if len(data) == 0 {
		return font, fmt.Errorf("SVG font file is empty")
	}
	font.data = base64.StdEncoding.EncodeToString(data)
	return font, nil
}

func (vhs *VHS) installSVGFont() error {
	path := vhs.Options.SVG.FontFile
	if path == "" {
		path = os.Getenv("VHS_SVG_FONT_FILE")
	}
	if path == "" {
		return nil
	}
	font, err := readSVGFont(path)
	if err != nil {
		return err
	}
	url := "data:" + font.mime + ";base64," + font.data
	_, err = vhs.Page.Eval(`async (family, url) => {
		const font = new FontFace(family, 'url("' + url + '")');
		await font.load();
		document.fonts.add(font);
	}`, captureFontFamily, url)
	if err != nil {
		return fmt.Errorf("failed to load SVG font in capture browser: %w", err)
	}
	vhs.svgFont = font
	vhs.Options.FontFamily = captureFontFamily
	return nil
}
