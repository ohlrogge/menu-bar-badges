package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"claude-quota/internal/badge"
)

// Badge geometry (pixels at 2x retina scale), matching the pr-review badge.
const (
	badgeW    = 52
	badgeH    = 28
	badgeR    = 5
	glyphZone = 18 // left zone for the database icon
	numZone   = 34 // right zone for the load number / ! / ?
)

// loadColor escalates with DB Load (Average Active Sessions) against the
// issue's thresholds. This is a raw AAS value, not a percentage.
func loadColor(load float64) color.NRGBA {
	switch {
	case load < 10:
		return badge.ColorGreen
	case load < 15:
		return badge.ColorOrange
	default:
		return badge.ColorRed
	}
}

// menuBarImage renders a single badge: a D glyph on the left and the highest
// DB Load across all instances (or ! on error, ? with no data) on the right.
func menuBarImage(maxLoad float64, hasData, hasErr bool) (string, error) {
	const (
		height = 32
		badgeY = (height - badgeH) / 2
	)

	img := image.NewNRGBA(image.Rect(0, 0, badgeW, height))

	var bg color.NRGBA
	switch {
	case hasErr:
		bg = badge.ColorErrOutline
	case !hasData:
		bg = badge.ColorGray
	default:
		bg = loadColor(maxLoad)
	}
	badge.DrawFilledRoundedRect(img, 0, badgeY, badgeW, badgeY+badgeH, badgeR, bg)

	glyphY := badgeY + (badgeH-14)/2

	// Database icon centred in the left zone.
	badge.DrawGlyph(img, (glyphZone-10)/2, glyphY, "DB", badge.ColorWhite, 2)

	// Load number (or ! / ?) centred in the right zone. No % symbol anywhere.
	var numStr string
	switch {
	case hasErr:
		numStr = "!"
	case !hasData:
		numStr = "?"
	default:
		numStr = fmt.Sprintf("%.0f", maxLoad)
	}
	runes := []rune(numStr)
	numTextW := len(runes)*10 + max(0, len(runes)-1)*2
	tx := glyphZone + (numZone-numTextW)/2
	for _, ch := range runes {
		badge.DrawLetter(img, tx, glyphY, ch, badge.ColorWhite, 2)
		tx += 12
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(badge.InjectPhysChunk(buf.Bytes())), nil
}
