package visualdiff

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
)

// colorDistanceThreshold is the minimum per-pixel RGB Euclidean
// distance (0 to ~441.7, sqrt(255^2*3)) for a pixel to count as
// "changed" rather than anti-aliasing/compression noise. Not
// user-configurable in v1 — see docs/VISUAL_REGRESSION.md.
const colorDistanceThreshold = 30.0

// Result is the outcome of comparing two screenshots.
type Result struct {
	// DiffPNG highlights changed pixels in solid red over a dimmed
	// grayscale rendering of the current screenshot elsewhere — the
	// same visual convention as Percy/reg-suit, not an invented one.
	DiffPNG []byte
	// PercentChanged is 0-100. Exactly 0 means pixel-identical.
	PercentChanged float64
}

// Compare diffs baseline against current, both full PNG-encoded
// screenshots. Mismatched dimensions never error — a layout change is
// exactly the kind of thing this should catch, not crash on; the
// non-overlapping region simply counts as fully changed.
func Compare(baseline, current []byte) (Result, error) {
	baseImg, err := png.Decode(bytes.NewReader(baseline))
	if err != nil {
		return Result{}, fmt.Errorf("visualdiff: decoding baseline screenshot: %w", err)
	}
	curImg, err := png.Decode(bytes.NewReader(current))
	if err != nil {
		return Result{}, fmt.Errorf("visualdiff: decoding current screenshot: %w", err)
	}

	baseBounds := baseImg.Bounds()
	curBounds := curImg.Bounds()
	width := maxInt(baseBounds.Dx(), curBounds.Dx())
	height := maxInt(baseBounds.Dy(), curBounds.Dy())

	diff := image.NewRGBA(image.Rect(0, 0, width, height))
	var changed, total int64

	for y := 0; y < height; y++ {
		inBaseRow := y < baseBounds.Dy()
		inCurRow := y < curBounds.Dy()
		for x := 0; x < width; x++ {
			total++
			inBase := inBaseRow && x < baseBounds.Dx()
			inCur := inCurRow && x < curBounds.Dx()

			if !inBase || !inCur {
				// Only one image has pixels here (dimensions differ) —
				// definitely a change.
				changed++
				diff.Set(x, y, color.RGBA{R: 255, A: 255})
				continue
			}

			br, bg, bb, _ := baseImg.At(baseBounds.Min.X+x, baseBounds.Min.Y+y).RGBA()
			cr, cg, cb, _ := curImg.At(curBounds.Min.X+x, curBounds.Min.Y+y).RGBA()
			if colorDistance(br, bg, bb, cr, cg, cb) > colorDistanceThreshold {
				changed++
				diff.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				diff.Set(x, y, dim(cr, cg, cb))
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, diff); err != nil {
		return Result{}, fmt.Errorf("visualdiff: encoding diff image: %w", err)
	}

	var percent float64
	if total > 0 {
		percent = (float64(changed) / float64(total)) * 100
	}
	return Result{DiffPNG: buf.Bytes(), PercentChanged: percent}, nil
}

// colorDistance takes color/RGBA()'s 16-bit channel values (0-65535)
// and returns the Euclidean distance in 8-bit RGB space.
func colorDistance(r1, g1, b1, r2, g2, b2 uint32) float64 {
	dr := float64(int32(r1>>8) - int32(r2>>8))
	dg := float64(int32(g1>>8) - int32(g2>>8))
	db := float64(int32(b1>>8) - int32(b2>>8))
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// dim renders an unchanged pixel as a faded grayscale version of the
// current screenshot — enough to show page layout for context without
// competing visually with the red-highlighted changes.
func dim(r, g, b uint32) color.RGBA {
	gray := 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)
	v := uint8(gray * 0.4)
	return color.RGBA{R: v, G: v, B: v, A: 255}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
