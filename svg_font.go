package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	captureFontFamily = "VHS Capture Font"
	fontMIMETTF       = "font/ttf"
	fontMIMEOTF       = "font/otf"
	fontFormatTTF     = "truetype"
	fontFormatOTF     = "opentype"
	fontFormatWOFF2   = "woff2"
)

type svgFont struct {
	data   string
	mime   string
	format string
	auto   bool
}

func readSVGFont(path string) (svgFont, error) {
	var font svgFont
	switch strings.ToLower(filepath.Ext(path)) {
	case ".woff2":
		font.mime, font.format = "font/woff2", fontFormatWOFF2
	case ".woff":
		font.mime, font.format = "font/woff", "woff"
	case ".ttf":
		font.mime, font.format = fontMIMETTF, fontFormatTTF
	case ".otf":
		font.mime, font.format = fontMIMEOTF, fontFormatOTF
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
	if path == "" && vhs.Options.Video.Output.SVG == "" {
		return nil
	}
	ctx := vhs.Page.GetContext()
	font, err := selectSVGFont(ctx, path, vhs.Options.FontFamily)
	if err != nil {
		return err
	}
	if font.data != "" {
		if err := vhs.loadSVGFont(captureFontFamily, font); err != nil {
			return err
		}
		vhs.svgFont = font
		if font.auto {
			vhs.svgOriginalFamily = vhs.Options.FontFamily
		}
		vhs.Options.FontFamily = captureFontFamily
	}
	if vhs.Options.Video.Output.SVG == "" {
		return nil
	}
	style := vhs.Options.Video.Style
	if style.WindowBar != "" && style.WindowBarTitle != "" && style.WindowBarFontFamily != "" {
		title, err := selectSVGFont(ctx, "", style.WindowBarFontFamily)
		if err != nil {
			return err
		}
		if title.data != "" {
			if err := vhs.loadSVGFont(titleFontFamily, title); err != nil {
				return err
			}
			vhs.svgTitleFont = title
		}
	}
	return nil
}

func (vhs *VHS) loadSVGFont(family string, font svgFont) error {
	url := "data:" + font.mime + ";base64," + font.data
	_, err := vhs.Page.Eval(`async (family, url) => {
		const font = new FontFace(family, 'url("' + url + '")');
		await font.load();
		document.fonts.add(font);
	}`, family, url)
	if err != nil {
		return fmt.Errorf("failed to load SVG font in capture browser: %w", err)
	}
	return nil
}

func selectSVGFont(ctx context.Context, path, family string) (svgFont, error) {
	if path != "" {
		return readSVGFont(path)
	}
	return discoverSVGFont(ctx, family)
}

func fontDiagnostic(family, message string) {
	log.Printf("SVG font %q: %s", family, message)
}
