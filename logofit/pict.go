package logofit

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

// ExtractStrokes returns, for a pictogram image (white strokes on a dark
// tile), the Otsu threshold and a binary mask of the strokes: the non-dark
// pixels inside the tile's bounding box that a 4-connected flood from the
// box border cannot reach (the tile's rounded corners are reachable, so
// they are excluded).
func ExtractStrokes(lum Cov) (int, Cov, error) {
	t := Otsu(lum)
	dark := Dark(lum, t)
	comps := Components(dark)
	if len(comps) == 0 {
		return t, Cov{}, fmt.Errorf("no dark component")
	}
	tile := comps[0]
	for _, c := range comps {
		if c.Area > tile.Area {
			tile = c
		}
	}
	outside := make([]bool, len(lum.V))
	var stack []int
	push := func(x, y int) {
		if x < tile.MinX || x > tile.MaxX || y < tile.MinY || y > tile.MaxY {
			return
		}
		p := y*lum.W + x
		if !outside[p] && dark.V[p] < 0.5 {
			outside[p] = true
			stack = append(stack, p)
		}
	}
	for x := tile.MinX; x <= tile.MaxX; x++ {
		push(x, tile.MinY)
		push(x, tile.MaxY)
	}
	for y := tile.MinY; y <= tile.MaxY; y++ {
		push(tile.MinX, y)
		push(tile.MaxX, y)
	}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := p%lum.W, p/lum.W
		push(x-1, y)
		push(x+1, y)
		push(x, y-1)
		push(x, y+1)
	}
	m := NewCov(lum.W, lum.H)
	for y := tile.MinY; y <= tile.MaxY; y++ {
		for x := tile.MinX; x <= tile.MaxX; x++ {
			p := y*lum.W + x
			if dark.V[p] < 0.5 && !outside[p] {
				m.V[p] = 1
			}
		}
	}
	return t, m, nil
}

// WriteDiff writes the original mask against a redraw at 8x: white where
// both agree, red only in the original, cyan only in the redraw.
func WriteDiff(path string, orig Cov, s Shape, a Affine) error {
	const k = 8
	// the redraw is rasterised directly on the 8x grid (scaled affine)
	big := Render(s, Affine{a.Sx * k, a.Sy * k, a.Ox * k, a.Oy * k}, orig.W*k, orig.H*k, 1)
	img := image.NewRGBA(image.Rect(0, 0, orig.W*k, orig.H*k))
	for y := 0; y < orig.H*k; y++ {
		for x := 0; x < orig.W*k; x++ {
			o := orig.V[(y/k)*orig.W+x/k] >= 0.5
			r := big.V[y*big.W+x] >= 0.5
			c := color.RGBA{20, 20, 20, 255}
			switch {
			case o && r:
				c = color.RGBA{255, 255, 255, 255}
			case o:
				c = color.RGBA{230, 40, 40, 255}
			case r:
				c = color.RGBA{0, 200, 230, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
