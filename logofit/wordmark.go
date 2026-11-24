package logofit

import (
	"fmt"
	"sort"
	"sync"
)

// Pad is the margin, in pixels, of each fitting window around its target.
const Pad = 10

// Window is a target mask cropped from the full image.
type Window struct {
	X0, Y0 int
	Mask   Cov
}

// Wordmark is the isolated "CCDPS" of a lockup.
type Wordmark struct {
	Threshold            int
	W, H                 int
	Tile                 *Comp // nil when the image has no tile
	LetterTop, LetterB   int   // rows spanned by the letters
	SmallComps           int   // components right of the tile shorter than 60% of the letters (baseline text)
	SmallTop, SmallBot   int
	SmallMinX, SmallMaxX int // x extent of the baseline text
	Letters              [5]Window
	Word                 Window
}

func window(m Cov, x0, y0, x1, y1 int) Window {
	x0, y0 = max(0, x0-Pad), max(0, y0-Pad)
	x1, y1 = min(m.W-1, x1+Pad), min(m.H-1, y1+Pad)
	return Window{x0, y0, Crop(m, x0, y0, x1-x0+1, y1-y0+1)}
}

// ExtractWordmark binarises lum with Otsu, finds the tile (a component
// spanning >= 20% of the width and >= 60% of the height), then keeps the
// components right of it whose height is >= 60% of the tallest one: those
// must be exactly 5, the letters, ordered left to right. Everything shorter
// is the baseline text.
func ExtractWordmark(lum Cov) (*Wordmark, error) {
	t := Otsu(lum)
	dark := Dark(lum, t)
	comps := Components(dark)
	wm := &Wordmark{Threshold: t, W: lum.W, H: lum.H}
	var cand []Comp
	for i := range comps {
		c := comps[i]
		if c.Width()*5 >= lum.W && c.Height()*10 >= lum.H*6 {
			cc := c
			wm.Tile = &cc
		}
	}
	tallest := 0
	for _, c := range comps {
		if wm.Tile != nil && (c.MinX <= wm.Tile.MaxX) {
			continue
		}
		cand = append(cand, c)
		tallest = max(tallest, c.Height())
	}
	var letters []Comp
	wm.SmallTop, wm.SmallMinX = lum.H, lum.W
	for _, c := range cand {
		if c.Height()*10 >= tallest*6 {
			letters = append(letters, c)
		} else {
			wm.SmallComps++
			wm.SmallTop, wm.SmallBot = min(wm.SmallTop, c.MinY), max(wm.SmallBot, c.MaxY)
			wm.SmallMinX, wm.SmallMaxX = min(wm.SmallMinX, c.MinX), max(wm.SmallMaxX, c.MaxX)
		}
	}
	if len(letters) != 5 {
		return nil, fmt.Errorf("found %d letter components, want 5", len(letters))
	}
	sort.Slice(letters, func(i, j int) bool { return letters[i].MinX < letters[j].MinX })
	union := NewCov(lum.W, lum.H)
	ux0, uy0, ux1, uy1 := lum.W, lum.H, -1, -1
	wm.LetterTop, wm.LetterB = lum.H, 0
	for i, c := range letters {
		m := c.Mask(lum.W, lum.H)
		for _, p := range c.Pix {
			union.V[p] = 1
		}
		wm.Letters[i] = window(m, c.MinX, c.MinY, c.MaxX, c.MaxY)
		ux0, uy0, ux1, uy1 = min(ux0, c.MinX), min(uy0, c.MinY), max(ux1, c.MaxX), max(uy1, c.MaxY)
		wm.LetterTop, wm.LetterB = min(wm.LetterTop, c.MinY), max(wm.LetterB, c.MaxY)
	}
	wm.Word = window(union, ux0, uy0, ux1, uy1)
	return wm, nil
}

// Result is the score of one font against a wordmark.
type Result struct {
	Family     string
	Weight     int
	GlyphIoU   [5]float64 // uniform scale
	MeanIoU    float64
	MeanIoUXY  float64 // independent x/y scale within +-8%
	Chamfer    float64 // mean over glyphs, uniform
	ChamferXY  float64
	WordIoU    float64
	WordChamf  float64
	WordSize   float64 // fitted em size, px
	WordOx     float64 // fitted pen origin of the first letter, image px
	WordOy     float64 // fitted baseline, image px
	LetterSize [5]float64
}

// XYTol is the allowed anisotropy of the x/y scale fit.
const XYTol = 0.08

// Score fits every letter (uniform and xy) and the whole word.
func Score(wm *Wordmark, f *Font) Result {
	r := Result{Family: f.Family, Weight: f.Weight}
	for i, ch := range "CCDPS" {
		run := f.Run(string(ch))
		u := FitShape(run, wm.Letters[i].Mask, Uniform, 0)
		x := FitShape(run, wm.Letters[i].Mask, XY, XYTol)
		r.GlyphIoU[i] = u.IoU
		r.MeanIoU += u.IoU / 5
		r.MeanIoUXY += x.IoU / 5
		r.Chamfer += u.Chamfer / 5
		r.ChamferXY += x.Chamfer / 5
		r.LetterSize[i] = u.Sx * unitPPEM
	}
	w := FitShape(f.Run("CCDPS"), wm.Word.Mask, Uniform, 0)
	r.WordIoU, r.WordChamf = w.IoU, w.Chamfer
	r.WordSize = w.Sx * unitPPEM
	r.WordOx, r.WordOy = w.Aff.Ox+float64(wm.Word.X0), w.Aff.Oy+float64(wm.Word.Y0)
	return r
}

// Rank scores all fonts (4 workers) and sorts by mean per-glyph IoU.
func Rank(wm *Wordmark, fonts []*Font, workers int) []Result {
	res := make([]Result, len(fonts))
	var wg sync.WaitGroup
	ch := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				res[i] = Score(wm, fonts[i])
			}
		}()
	}
	for i := range fonts {
		ch <- i
	}
	close(ch)
	wg.Wait()
	sort.SliceStable(res, func(i, j int) bool { return res[i].MeanIoU > res[j].MeanIoU })
	return res
}

// SynthLockup renders "CCDPS" in f at size px with the pen at (ox,oy) on a
// white w x h image, next to a black tile, and returns its luminance: a
// stand-in for the original used as a positive control.
func SynthLockup(f *Font, w, h int, size, ox, oy float64) Cov {
	ink := Render(f.Run("CCDPS"), Affine{size / unitPPEM, size / unitPPEM, ox, oy}, w, h, SS)
	lum := NewCov(w, h)
	for i, v := range ink.V {
		lum.V[i] = 255 * (1 - v)
	}
	for y := 8; y < h-8; y++ {
		for x := 8; x < 136 && x < w; x++ {
			lum.V[y*w+x] = 20
		}
	}
	return lum
}
