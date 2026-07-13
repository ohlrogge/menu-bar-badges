// Package badge holds the content-agnostic menu bar rendering primitives
// shared by the SwiftBar plugins in this repo: a 5×7 pixel font, the colour
// palette, rounded-rect/letter drawing, and a pHYs-chunk injector that makes
// the PNGs render at retina density.
package badge

import "image/color"

// Palette shared by the badges. The error/orange outline colour is kept as a
// distinct name even though it equals Orange, to preserve intent at call sites.
var (
	ColorErrOutline  = color.NRGBA{R: 255, G: 159, B: 10, A: 255}
	ColorGreen       = color.NRGBA{R: 52, G: 199, B: 89, A: 255}
	ColorYellow      = color.NRGBA{R: 255, G: 214, B: 10, A: 255}
	ColorOrange      = color.NRGBA{R: 255, G: 159, B: 10, A: 255}
	ColorRed         = color.NRGBA{R: 255, G: 59, B: 48, A: 255}
	ColorBlue        = color.NRGBA{R: 10, G: 132, B: 255, A: 255}
	ColorGray        = color.NRGBA{R: 142, G: 142, B: 147, A: 255}
	ColorBlack       = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	ColorWhite       = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	ColorTransparent = color.NRGBA{R: 0, G: 0, B: 0, A: 0}
)

// Glyphs is a 5×7 pixel font; each entry is 7 rows of 5 columns ('X' = on,
// any other character = off).
var Glyphs = map[string][]string{
	"!": {"..X..", "..X..", "..X..", "..X..", "..X..", ".....", "..X.."},
	"*": {"..X..", "X.X.X", ".XXX.", "..X..", ".XXX.", "X.X.X", "..X.."},
	"?": {".XXX.", "X...X", "....X", "...X.", "..X..", ".....", "..X.."},
	"A": {".XXX.", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"},
	"B": {"XXXX.", "X...X", "X...X", "XXXX.", "X...X", "X...X", "XXXX."},
	"C": {".XXX.", "X...X", "X....", "X....", "X....", "X...X", ".XXX."},
	"D": {"XXXX.", "X...X", "X...X", "X...X", "X...X", "X...X", "XXXX."},
	"E": {"XXXXX", "X....", "X....", "XXXX.", "X....", "X....", "XXXXX"},
	"F": {"XXXXX", "X....", "X....", "XXXX.", "X....", "X....", "X...."},
	"G": {".XXX.", "X...X", "X....", "X.XXX", "X...X", "X...X", ".XXX."},
	"H": {"X...X", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"},
	"I": {".XXX.", "..X..", "..X..", "..X..", "..X..", "..X..", ".XXX."},
	"J": {"..XXX", "...X.", "...X.", "...X.", "...X.", "X..X.", ".XX.."},
	"K": {"X...X", "X..X.", "X.X..", "XX...", "X.X..", "X..X.", "X...X"},
	"L": {"X....", "X....", "X....", "X....", "X....", "X....", "XXXXX"},
	"M": {"X...X", "XX.XX", "X.X.X", "X.X.X", "X...X", "X...X", "X...X"},
	"N": {"X...X", "XX..X", "X.X.X", "X..XX", "X...X", "X...X", "X...X"},
	"O": {".XXX.", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."},
	"P": {"XXXX.", "X...X", "X...X", "XXXX.", "X....", "X....", "X...."},
	"Q": {".XXX.", "X...X", "X...X", "X...X", "X.X.X", "X..X.", ".XX.X"},
	"R": {"XXXX.", "X...X", "X...X", "XXXX.", "X.X..", "X..X.", "X...X"},
	"S": {".XXXX", "X....", "X....", ".XXX.", "....X", "....X", "XXXX."},
	"T": {"XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "..X.."},
	"U": {"X...X", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."},
	"V": {"X...X", "X...X", "X...X", "X...X", "X...X", ".X.X.", "..X.."},
	"W": {"X...X", "X...X", "X...X", "X.X.X", "X.X.X", "X.X.X", ".X.X."},
	"X": {"X...X", "X...X", ".X.X.", "..X..", ".X.X.", "X...X", "X...X"},
	"Y": {"X...X", "X...X", ".X.X.", "..X..", "..X..", "..X..", "..X.."},
	"Z": {"XXXXX", "....X", "...X.", "..X..", ".X...", "X....", "XXXXX"},
	"0": {".XXX.", "X...X", "X..XX", "X.X.X", "XX..X", "X...X", ".XXX."},
	"1": {"..X..", ".XX..", "..X..", "..X..", "..X..", "..X..", ".XXX."},
	"2": {".XXX.", "X...X", "....X", "...X.", "..X..", ".X...", "XXXXX"},
	"3": {".XXX.", "X...X", "....X", "..XX.", "....X", "X...X", ".XXX."},
	"4": {"...X.", "..XX.", ".X.X.", "X..X.", "XXXXX", "...X.", "...X."},
	"5": {"XXXXX", "X....", "XXXX.", "....X", "....X", "X...X", ".XXX."},
	"6": {".XXX.", "X....", "XXXX.", "X...X", "X...X", "X...X", ".XXX."},
	"7": {"XXXXX", "....X", "...X.", "..X..", "..X..", "..X..", "..X.."},
	"8": {".XXX.", "X...X", "X...X", ".XXX.", "X...X", "X...X", ".XXX."},
	"9": {".XXX.", "X...X", "X...X", ".XXXX", "....X", "....X", ".XXX."},
	":": {".....", "..X..", "..X..", ".....", "..X..", "..X..", "....."},
	"+": {".....", "..X..", "..X..", "XXXXX", "..X..", "..X..", "....."},
	// "BRANCH": a multi-char key (never matched by single-rune letter lookups),
	// drawn via DrawGlyph. A solid trunk on the left with a feature branch
	// peeling off diagonally — the pull-request mental model.
	// The gap between trunk and branch reads as "not yet merged" — i.e. open.
	"BRANCH": {"X....", "X....", "X.X..", "X..X.", "X...X", "X...X", "X...X"},
	// "DB": a stacked-cylinder database/storage icon — the classic top
	// ellipse, two divider rings, and bottom ellipse — for rds-load.
	"DB": {".XXX.", "X...X", "XXXXX", "X...X", "XXXXX", "X...X", ".XXX."},
}
