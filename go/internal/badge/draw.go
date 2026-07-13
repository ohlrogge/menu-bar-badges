package badge

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"
)

// FillRect fills the half-open rectangle [x0,x1)×[y0,y1) with c.
func FillRect(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

// DrawFilledRoundedRect draws a solid rounded rectangle using circle-arc
// corners. r is the corner radius in pixels.
func DrawFilledRoundedRect(img *image.NRGBA, x0, y0, x1, y1, r int, c color.NRGBA) {
	// Fill the cross-shaped body (avoids double-drawing the corner boxes).
	FillRect(img, x0+r, y0, x1-r, y1, c)   // horizontal band
	FillRect(img, x0, y0+r, x0+r, y1-r, c) // left strip
	FillRect(img, x1-r, y0+r, x1, y1-r, c) // right strip
	// Fill corner arcs: for each pixel in the r×r corner box, include it if
	// its centre lies within the circle of radius r.
	for cy := 0; cy < r; cy++ {
		for cx := 0; cx < r; cx++ {
			dx, dy := r-1-cx, r-1-cy // distance of pixel centre from arc centre
			if dx*dx+dy*dy < r*r {
				img.SetNRGBA(x0+cx, y0+cy, c)     // top-left
				img.SetNRGBA(x1-1-cx, y0+cy, c)   // top-right
				img.SetNRGBA(x0+cx, y1-1-cy, c)   // bottom-left
				img.SetNRGBA(x1-1-cx, y1-1-cy, c) // bottom-right
			}
		}
	}
}

// DrawLetter renders one character glyph from the pixel font at (x0, y0) at the
// given integer scale. Unknown glyphs fall back to "I".
func DrawLetter(img *image.NRGBA, x0, y0 int, ch rune, c color.NRGBA, scale int) {
	DrawGlyph(img, x0, y0, strings.ToUpper(string(ch)), c, scale)
}

// DrawGlyph renders the glyph stored under key (e.g. "A" or "BRANCH") at
// (x0, y0). Multi-character keys allow icon glyphs that the single-rune letter
// lookup never reaches. Unknown keys fall back to "I".
func DrawGlyph(img *image.NRGBA, x0, y0 int, key string, c color.NRGBA, scale int) {
	rows, ok := Glyphs[key]
	if !ok {
		rows = Glyphs["I"] // fallback glyph
	}
	for r, row := range rows {
		for col, v := range row {
			if v == 'X' {
				FillRect(img,
					x0+col*scale, y0+r*scale,
					x0+(col+1)*scale, y0+(r+1)*scale, c)
			}
		}
	}
}

// Countdown returns a short time-remaining string (e.g. "2:15" or "3D") for an
// RFC3339 timestamp. Empty/invalid input yields "".
func Countdown(iso string) string {
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

// LastRefreshed formats a Unix-seconds timestamp as a short local clock time
// (e.g. "14:32") for display next to a "Refresh now" menu item. Zero/negative
// input yields "".
func LastRefreshed(unixSeconds float64) string {
	if unixSeconds <= 0 {
		return ""
	}
	return time.Unix(int64(unixSeconds), 0).Local().Format("15:04")
}
