package main

import "math"

func validSVGCellSize(size float64) bool {
	return size > 0 && !math.IsInf(size, 0) && !math.IsNaN(size)
}

// svgGridDimension retains all measured cells inside an integer pixel viewport.
func svgGridDimension(cells int, size float64, fallback int) int {
	if cells <= 0 || !validSVGCellSize(size) {
		return fallback
	}
	extent := math.Ceil(float64(cells) * size)
	if extent < 1 || extent >= float64(int(^uint(0)>>1)) {
		return fallback
	}
	return int(extent)
}
