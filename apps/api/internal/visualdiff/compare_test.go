package visualdiff

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// solidPNG encodes a w x h image filled with c, except for a
// rectangular patch [patchX, patchY, patchX+patchW, patchY+patchH)
// filled with patchColor (patchW/patchH 0 means no patch).
func solidPNG(t *testing.T, w, h int, c color.Color, patchX, patchY, patchW, patchH int, patchColor color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if patchW > 0 && patchH > 0 && x >= patchX && x < patchX+patchW && y >= patchY && y < patchY+patchH {
				img.Set(x, y, patchColor)
			} else {
				img.Set(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding test PNG: %v", err)
	}
	return buf.Bytes()
}

func TestCompare_IdenticalImages_ZeroPercentChanged(t *testing.T) {
	img := solidPNG(t, 20, 20, color.White, 0, 0, 0, 0, nil)

	result, err := Compare(img, img, DefaultColorDistanceThreshold)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if result.PercentChanged != 0 {
		t.Errorf("PercentChanged = %v, want 0", result.PercentChanged)
	}
}

func TestCompare_PartialPatch_ExactPercentage(t *testing.T) {
	// A 10x10 white image with a 5x2 black patch (10 of 100 pixels) —
	// black vs. white is far past colorDistanceThreshold, so the exact
	// expected result is deterministic: 10%.
	baseline := solidPNG(t, 10, 10, color.White, 0, 0, 0, 0, nil)
	current := solidPNG(t, 10, 10, color.White, 0, 0, 5, 2, color.Black)

	result, err := Compare(baseline, current, DefaultColorDistanceThreshold)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if result.PercentChanged != 10 {
		t.Errorf("PercentChanged = %v, want 10", result.PercentChanged)
	}
	if len(result.DiffPNG) == 0 {
		t.Error("DiffPNG is empty, want a rendered diff image")
	}
	// The diff image itself must decode back to a valid PNG of the same
	// canvas size.
	decoded, err := png.Decode(bytes.NewReader(result.DiffPNG))
	if err != nil {
		t.Fatalf("decoding DiffPNG: %v", err)
	}
	if decoded.Bounds().Dx() != 10 || decoded.Bounds().Dy() != 10 {
		t.Errorf("DiffPNG size = %v, want 10x10", decoded.Bounds())
	}
}

func TestCompare_MinorNoiseBelowThreshold_NotCountedAsChanged(t *testing.T) {
	// A color one 8-bit step away from white in each channel — well
	// under colorDistanceThreshold — must not register as a change
	// (anti-aliasing/compression noise tolerance).
	baseline := solidPNG(t, 10, 10, color.White, 0, 0, 0, 0, nil)
	nearWhite := color.RGBA{R: 254, G: 254, B: 254, A: 255}
	current := solidPNG(t, 10, 10, nearWhite, 0, 0, 0, 0, nil)

	result, err := Compare(baseline, current, DefaultColorDistanceThreshold)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if result.PercentChanged != 0 {
		t.Errorf("PercentChanged = %v, want 0 (within noise tolerance)", result.PercentChanged)
	}
}

func TestCompare_CustomThreshold_ChangesClassification(t *testing.T) {
	baseline := solidPNG(t, 10, 10, color.White, 0, 0, 0, 0, nil)
	// Exactly 20.0 RGB-distance from white (dr=20, dg=0, db=0).
	shifted := color.RGBA{R: 235, G: 255, B: 255, A: 255}
	current := solidPNG(t, 10, 10, shifted, 0, 0, 0, 0, nil)

	strict, err := Compare(baseline, current, 15)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if strict.PercentChanged != 100 {
		t.Errorf("PercentChanged at threshold=15 = %v, want 100 (a distance-20 delta exceeds a threshold of 15)", strict.PercentChanged)
	}

	lenient, err := Compare(baseline, current, 25)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if lenient.PercentChanged != 0 {
		t.Errorf("PercentChanged at threshold=25 = %v, want 0 (a distance-20 delta is under a threshold of 25)", lenient.PercentChanged)
	}
}

func TestCompare_MismatchedDimensions_NeverErrors(t *testing.T) {
	baseline := solidPNG(t, 10, 10, color.White, 0, 0, 0, 0, nil)
	current := solidPNG(t, 20, 10, color.White, 0, 0, 0, 0, nil)

	result, err := Compare(baseline, current, DefaultColorDistanceThreshold)
	if err != nil {
		t.Fatalf("Compare() error = %v, want no error for mismatched dimensions", err)
	}
	if result.PercentChanged <= 0 {
		t.Errorf("PercentChanged = %v, want > 0 (the extra width is unmatched)", result.PercentChanged)
	}
}

func TestCompare_InvalidPNG_Errors(t *testing.T) {
	if _, err := Compare([]byte("not a png"), []byte("not a png either"), DefaultColorDistanceThreshold); err == nil {
		t.Fatal("Compare() error = nil, want an error for invalid PNG input")
	}
}
