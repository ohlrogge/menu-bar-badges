package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"
)

var (
	colorErrOutline  = color.NRGBA{R: 255, G: 159, B: 10, A: 255}
	colorGreen       = color.NRGBA{R: 52, G: 199, B: 89, A: 255}
	colorYellow      = color.NRGBA{R: 255, G: 214, B: 10, A: 255}
	colorOrange      = color.NRGBA{R: 255, G: 159, B: 10, A: 255}
	colorRed         = color.NRGBA{R: 255, G: 59, B: 48, A: 255}
	colorBlack       = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	colorWhite       = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	colorTransparent = color.NRGBA{R: 0, G: 0, B: 0, A: 0}
)

// 5×7 pixel font; each entry is 7 rows of 5 columns ('X' = on, any other = off).
var glyphs = map[string][]string{
	"!": {"..X..", "..X..", "..X..", "..X..", "..X..", ".....", "..X.."},
	"*": {"..X..", "X.X.X", ".XXX.", "..X..", ".XXX.", "X.X.X", "..X.."},
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
}

func fillRect(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// drawFilledRoundedRect draws a solid rounded rectangle using circle-arc corners.
// r is the corner radius in pixels.
func drawFilledRoundedRect(img *image.NRGBA, x0, y0, x1, y1, r int, c color.NRGBA) {
	// Fill the cross-shaped body (avoids double-drawing the corner boxes).
	fillRect(img, x0+r, y0, x1-r, y1, c) // horizontal band
	fillRect(img, x0, y0+r, x0+r, y1-r, c) // left strip
	fillRect(img, x1-r, y0+r, x1, y1-r, c) // right strip
	// Fill corner arcs: for each pixel in the r×r corner box, include it if
	// its centre lies within the circle of radius r.
	for cy := 0; cy < r; cy++ {
		for cx := 0; cx < r; cx++ {
			dx, dy := r-1-cx, r-1-cy // distance of pixel centre from arc centre
			if dx*dx+dy*dy < r*r {
				img.SetNRGBA(x0+cx, y0+cy, c)       // top-left
				img.SetNRGBA(x1-1-cx, y0+cy, c)     // top-right
				img.SetNRGBA(x0+cx, y1-1-cy, c)     // bottom-left
				img.SetNRGBA(x1-1-cx, y1-1-cy, c)   // bottom-right
			}
		}
	}
}

// drawLetter renders one glyph from the pixel font at (x0, y0).
func drawLetter(img *image.NRGBA, x0, y0 int, ch rune, c color.NRGBA, scale int) {
	rows, ok := glyphs[strings.ToUpper(string(ch))]
	if !ok {
		rows = glyphs["I"] // fallback glyph
	}
	for r, row := range rows {
		for col, v := range row {
			if v == 'X' {
				fillRect(img,
					x0+col*scale, y0+r*scale,
					x0+(col+1)*scale, y0+(r+1)*scale, c)
			}
		}
	}
}

// countdown returns a short time-remaining string (e.g. "2:15" or "3D").
func countdown(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	left := time.Until(t)
	if left < 0 {
		left = 0
	}
	mins := int(left.Minutes())
	if mins >= 48*60 {
		return fmt.Sprintf("%dD", mins/1440)
	}
	return fmt.Sprintf("%d:%02d", mins/60, mins%60)
}

// BarResult is one gauge's input to menuBarImage.
type BarResult struct {
	Name   string
	Usage  *Usage
	HasErr bool
}

// Badge geometry (all measurements in pixels at 2× retina scale).
const (
	badgeW    = 52 // fixed badge width
	gaugeH    = 28 // badge height (retained name; used in menuBarImage)
	badgeR    = 5  // corner radius
	starZoneW = 18 // left zone reserved for the * glyph
	numZoneW  = 34 // right zone for the number / countdown
)

// drawBadge renders a solid rounded badge at (x, y).
//
// The badge is divided into two fixed zones: the left zone always shows *
// (the Claude icon shorthand) and the right zone shows the utilization number
// or countdown. On error, ! replaces the number; * stays in place.
// fillC overrides the automatic background colour (used for the weekly-lockout black fill).
func drawBadge(img *image.NRGBA, x, y int, utilization *float64, hasErr bool, text string, fillC *color.NRGBA) {
	// Resolve background colour.
	var bg color.NRGBA
	if hasErr {
		bg = colorErrOutline
	} else if fillC != nil {
		bg = *fillC
	} else if utilization != nil {
		u := *utilization
		switch {
		case u >= 90:
			bg = colorRed
		case u >= 75:
			bg = colorOrange
		case u >= 60:
			bg = colorYellow
		default:
			bg = colorGreen
		}
	} else {
		bg = colorGreen
	}

	// 1. Solid filled badge.
	drawFilledRoundedRect(img, x, y, x+badgeW, y+gaugeH, badgeR, bg)

	glyphY := y + (gaugeH-14)/2

	// 2. * always at fixed position, centred in the left zone.
	starX := x + (starZoneW-10)/2
	drawLetter(img, starX, glyphY, '*', colorWhite, 2)

	// 3. Number or ! centred in the right zone.
	var numStr string
	if hasErr {
		numStr = "!"
	} else if utilization == nil {
		return
	} else {
		numStr = text
		if numStr == "" {
			numStr = fmt.Sprintf("%.0f", *utilization)
		}
	}
	runes := []rune(numStr)
	numTextW := len(runes)*10 + max(0, len(runes)-1)*2
	tx := x + starZoneW + (numZoneW-numTextW)/2
	for _, ch := range runes {
		drawLetter(img, tx, glyphY, ch, colorWhite, 2)
		tx += 12
	}
}

// menuBarImage renders all visible badges as a retina-ready base64 PNG.
// Each badge is a solid coloured rounded rect with * on the left and the
// utilization percentage (or countdown) on the right.
func menuBarImage(results []BarResult, showLetters bool) (string, error) {
	const (
		letterW = 10
		gap     = 4
		cellGap = 8
		height  = 32
		gaugeY  = (height - gaugeH) / 2 // centres the 24 px badge in the 32 px canvas
	)

	n := len(results)
	labelW := 0
	if showLetters {
		labelW = letterW + gap
	}
	cellW := labelW + badgeW

	width := 0
	if n > 0 {
		width = n*cellW + (n-1)*cellGap
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))

	for i, r := range results {
		x := i * (cellW + cellGap)

		var util *float64
		var text string
		var fillC *color.NRGBA

		if r.Usage != nil {
			five := r.Usage.FiveHour
			week := r.Usage.SevenDay

			if five != nil {
				v := five.Utilization
				util = &v
			}
			if week != nil && week.Utilization >= 100 {
				v := 100.0
				util = &v
				c := colorBlack
				fillC = &c
				if cd := countdown(week.ResetsAt); cd != "" {
					text = cd
				}
			} else if five != nil && five.Utilization >= 100 {
				if cd := countdown(five.ResetsAt); cd != "" {
					text = cd
				}
			}
		}

		if showLetters {
			if runes := []rune(r.Name); len(runes) > 0 {
				drawLetter(img, x, (height-14)/2, runes[0], colorWhite, 2)
			}
		}
		drawBadge(img, x+labelW, gaugeY, util, r.HasErr, text, fillC)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(injectPhysChunk(buf.Bytes())), nil
}

// injectPhysChunk splices a pHYs chunk (144 dpi, 2× retina) after IHDR.
func injectPhysChunk(data []byte) []byte {
	const sigLen = 8
	if len(data) < sigLen+12 {
		return data
	}
	ihdrDataLen := int(binary.BigEndian.Uint32(data[sigLen:]))
	insertAt := sigLen + 4 + 4 + ihdrDataLen + 4

	const density uint32 = 5669 // pixels/metre ≈ 144 dpi
	chunkType := []byte("pHYs")
	chunkData := make([]byte, 9)
	binary.BigEndian.PutUint32(chunkData[0:], density)
	binary.BigEndian.PutUint32(chunkData[4:], density)
	chunkData[8] = 1 // unit = metre

	lenBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBytes, uint32(len(chunkData)))

	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes,
		crc32.ChecksumIEEE(append(chunkType, chunkData...)))

	chunk := make([]byte, 0, 4+4+9+4)
	chunk = append(chunk, lenBytes...)
	chunk = append(chunk, chunkType...)
	chunk = append(chunk, chunkData...)
	chunk = append(chunk, crcBytes...)

	out := make([]byte, 0, len(data)+len(chunk))
	out = append(out, data[:insertAt]...)
	out = append(out, chunk...)
	out = append(out, data[insertAt:]...)
	return out
}
