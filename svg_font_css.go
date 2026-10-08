package main

import (
	"fmt"
	"strings"
	"unicode"
)

func cssFontString(family string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range family {
		switch r {
		case '\\', '"', '{', '}', ';':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			if r < 0x20 || r == 0x7f || r == '<' || r == '>' || r == '&' {
				_, _ = fmt.Fprintf(&b, "\\%X ", r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func cssFontFamily(family string) string {
	for _, r := range family {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && r != '-' && r != '_' {
			return cssFontString(family)
		}
	}
	return family
}

func (font svgFont) cssFace(family string) string {
	if font.data == "" {
		return ""
	}
	if font.mime == "" {
		font.mime = fontMIMETTF
	}
	if font.format == "" {
		font.format = fontFormatTTF
		if strings.Contains(font.mime, fontFormatWOFF2) {
			font.format = fontFormatWOFF2
		} else if strings.Contains(font.mime, "woff") {
			font.format = "woff"
		} else if font.mime == fontMIMEOTF || font.mime == "font/opentype" {
			font.format = fontFormatOTF
		}
	}
	return fmt.Sprintf(`@font-face { font-family: %s; src: url("data:%s;base64,%s") format("%s"); }`,
		cssFontString(family), font.mime, font.data, font.format)
}
