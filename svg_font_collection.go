package main

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// extractSVGFontFace turns one collection face into a standalone SFNT file.
func extractSVGFontFace(data []byte, index int) ([]byte, error) {
	if len(data) < 12 || string(data[:4]) != "ttcf" {
		return nil, errors.New("invalid SVG font collection header")
	}
	count := uint64(binary.BigEndian.Uint32(data[8:12]))
	if index < 0 || uint64(index) >= count || 12+4*count > uint64(len(data)) {
		return nil, fmt.Errorf("SVG font collection face index %d is out of range", index)
	}
	offset := uint64(binary.BigEndian.Uint32(data[12+4*index:]))
	if offset+12 > uint64(len(data)) {
		return nil, errors.New("invalid SVG font collection face offset")
	}
	face := data[offset:]
	count = uint64(binary.BigEndian.Uint16(face[4:6]))
	directorySize := 12 + 16*count
	if count == 0 || directorySize > uint64(len(face)) {
		return nil, errors.New("invalid SVG font collection table directory")
	}
	result := append([]byte(nil), face[:directorySize]...)
	headOffset := -1
	for i := range int(count) {
		record := result[12+16*i : 28+16*i]
		start := uint64(binary.BigEndian.Uint32(record[8:12]))
		length := uint64(binary.BigEndian.Uint32(record[12:16]))
		if start+length > uint64(len(data)) {
			return nil, errors.New("invalid SVG font collection table bounds")
		}
		binary.BigEndian.PutUint32(record[8:12], uint32(len(result)))
		if string(record[:4]) == "head" {
			if length < 12 {
				return nil, errors.New("invalid SVG font collection head table")
			}
			headOffset = len(result)
		}
		result = append(result, data[start:start+length]...)
		for len(result)%4 != 0 {
			result = append(result, 0)
		}
	}
	if headOffset >= 0 {
		binary.BigEndian.PutUint32(result[headOffset+8:], 0)
		var checksum uint32
		for i := 0; i < len(result); i += 4 {
			checksum += binary.BigEndian.Uint32(result[i:])
		}
		binary.BigEndian.PutUint32(result[headOffset+8:], 0xb1b0afba-checksum)
	}
	return result, nil
}
