package main

import (
	"fmt"
	"image"
	"io"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ccdps/brand/logofit"
)

// medianColour returns the per-channel median RGB of the pixels of a
// full-alpha, binary mask in the PNG at path.
func medianColour(path string, mask logofit.Cov) (r, g, b uint8, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, 0, 0, err
	}
	var rs, gs, bs []int
	bd := img.Bounds()
	for y := 0; y < mask.H; y++ {
		for x := 0; x < mask.W; x++ {
			if mask.V[y*mask.W+x] < 0.5 {
				continue
			}
			pr, pg, pb, pa := img.At(bd.Min.X+x, bd.Min.Y+y).RGBA()
			if pa != 0xffff {
				continue
			}
			rs, gs, bs = append(rs, int(pr>>8)), append(gs, int(pg>>8)), append(bs, int(pb>>8))
		}
	}
	if len(rs) == 0 {
		return 0, 0, 0, fmt.Errorf("no opaque pixels in the mask")
	}
	med := func(v []int) uint8 { sort.Ints(v); return uint8(v[len(v)/2]) }
	return med(rs), med(gs), med(bs), nil
}

type measureOpts struct {
	lockup string
	font   string
}

func measureCmd() *cobra.Command {
	var o measureOpts
	c := &cobra.Command{Use: "measure", Short: "print the lockup proportions and per-letter placements of a font", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return measure(o, cmd.OutOrStdout()) }}
	c.Flags().StringVar(&o.lockup, "lockup", "", "original lockup PNG (read only; required)")
	c.Flags().StringVar(&o.font, "font", "fonts/Figtree-700.ttf", "font file to place the letters with")
	_ = c.MarkFlagRequired("lockup")
	return c
}

func measure(o measureOpts, w io.Writer) error {
	lum, err := logofit.LoadLum(o.lockup)
	if err != nil {
		return err
	}
	wm, err := logofit.ExtractWordmark(lum)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "image %dx%d, Otsu %d\n", wm.W, wm.H, wm.Threshold)
	t := wm.Tile
	fmt.Fprintf(w, "tile: x %d..%d (w %d), y %d..%d (h %d), area %d\n", t.MinX, t.MaxX, t.Width(), t.MinY, t.MaxY, t.Height(), t.Area)
	fmt.Fprintf(w, "letters: rows %d..%d\n", wm.LetterTop, wm.LetterB)
	fmt.Fprintf(w, "baseline text: x %d..%d, rows %d..%d (cap height %d)\n", wm.SmallMinX, wm.SmallMaxX, wm.SmallTop, wm.SmallBot, wm.SmallBot-wm.SmallTop+1)
	f, err := logofit.LoadFont("Figtree", 700, o.font)
	if err != nil {
		return err
	}
	var em, ox, oy [5]float64
	for i, ch := range "CCDPS" {
		fit := logofit.FitShape(f.Run(string(ch)), wm.Letters[i].Mask, logofit.Uniform, 0)
		em[i], ox[i], oy[i] = fit.Sx*logofit.UnitsPerEm, fit.Aff.Ox+float64(wm.Letters[i].X0), fit.Aff.Oy+float64(wm.Letters[i].Y0)
		fmt.Fprintf(w, "letter %c: em %.3f px, pen origin x %.3f, baseline y %.3f, IoU %.4f\n", ch, em[i], ox[i], oy[i], fit.IoU)
	}
	var mem, mby, mtr float64
	for i, ch := range "CCDP" {
		mtr += ((ox[i+1]-ox[i])/em[i] - f.Advance(ch)/logofit.UnitsPerEm) / 4
	}
	for i := range em {
		mem += em[i] / 5
		mby += oy[i] / 5
	}
	fmt.Fprintf(w, "mean em %.3f px, mean baseline y %.3f, implied tracking %.4f em (pitch minus advance, per pair, averaged)\n", mem, mby, mtr)
	fmt.Fprintf(w, "first pen origin to tile right edge: %.3f px; cap-height/tile-height %.4f\n", ox[0]-float64(t.MaxX+1), float64(wm.LetterB-wm.LetterTop+1)/float64(t.Height()))
	tc := t.Mask(wm.W, wm.H)
	if r, g, b, err := medianColour(o.lockup, tc); err == nil {
		fmt.Fprintf(w, "tile colour (median of opaque pixels): #%02x%02x%02x\n", r, g, b)
	}
	word := logofit.NewCov(wm.W, wm.H)
	for _, l := range wm.Letters {
		for y := 0; y < l.Mask.H; y++ {
			for x := 0; x < l.Mask.W; x++ {
				if l.Mask.V[y*l.Mask.W+x] >= 0.5 {
					word.V[(l.Y0+y)*wm.W+l.X0+x] = 1
				}
			}
		}
	}
	if r, g, b, err := medianColour(o.lockup, word); err == nil {
		fmt.Fprintf(w, "wordmark colour (median of opaque pixels): #%02x%02x%02x\n", r, g, b)
	}
	return nil
}
