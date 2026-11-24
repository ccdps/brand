package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ccdps/brand/logofit"
)

type traceOpts struct {
	mark, v2, out, svgOut string
	jitters               int
}

func traceCmd() *cobra.Command {
	var o traceOpts
	c := &cobra.Command{Use: "trace", Short: "fit the pictogram geometry (v3) to the original by maximising IoU", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return trace(o, cmd.OutOrStdout()) }}
	f := c.Flags()
	f.StringVar(&o.mark, "mark", "", "original pictogram PNG (read only; required, not in the repository)")
	_ = c.MarkFlagRequired("mark")
	f.StringVar(&o.v2, "v2", "testdata/ccdps-mark-v2.svg", "starting SVG")
	f.StringVar(&o.out, "out", "out", "output directory")
	f.StringVar(&o.svgOut, "svg-out", "ccdps-mark-v3.svg", "where to write the v3 SVG")
	f.IntVar(&o.jitters, "jitters", 3, "number of jittered restarts")
	return c
}

func binarise(r logofit.Cov) logofit.Cov {
	b := logofit.NewCov(r.W, r.H)
	for i, v := range r.V {
		if v >= 0.5 {
			b.V[i] = 1
		}
	}
	return b
}

func trace(o traceOpts, stdout io.Writer) error {
	// a failed MkdirAll is reported by the os.Create below, naming the path.
	_ = os.MkdirAll(o.out, 0o755)
	rep, err := os.Create(filepath.Join(o.out, "trace-report.md"))
	if err != nil {
		return err
	}
	defer rep.Close()
	w := io.MultiWriter(stdout, rep)

	lum, err := logofit.LoadLum(o.mark)
	if err != nil {
		return err
	}
	_, mask, err := logofit.ExtractStrokes(lum)
	if err != nil {
		return err
	}
	lum2 := logofit.Upscale2(lum)
	// lum2 is lum upscaled, so it has the dark tile ExtractStrokes found above:
	// this second extraction cannot fail where the first succeeded.
	_, mask2, _ := logofit.ExtractStrokes(lum2)

	// v2 reference
	v2, err := logofit.LoadSVGStrokes(o.v2)
	if err != nil {
		return err
	}
	f2 := logofit.FitShape(v2, mask, logofit.Uniform, 0)
	h2 := logofit.IoU(mask2, logofit.Render(v2, f2.Aff.Double(), mask2.W, mask2.H, logofit.SS))
	fmt.Fprintf(w, "# v3 trace\n\nv2 (uniform fit): IoU %.4f chamfer %.3f px, scale %.4f; held-out x2 IoU %.4f\n", f2.IoU, f2.Chamfer, f2.Sx, h2)

	start := logofit.Params{G: logofit.V2Geo, S: f2.Sx, Ox: f2.Aff.Ox, Oy: f2.Aff.Oy}
	s0 := logofit.IoU(mask, logofit.Render(start.G.Shape(), start.Aff(), mask.W, mask.H, logofit.SS))
	fmt.Fprintf(w, "Geo(V2) at the v2 fit: IoU %.4f (base ends flush instead of 42/209.5)\n\n", s0)

	best, all := logofit.Trace(mask, start, o.jitters)
	for _, r := range all {
		fmt.Fprintf(w, "run %-9s IoU %.5f width %.3f scale %.4f\n", r.Start, r.IoU, r.P.G[logofit.W], r.P.S)
	}
	fmt.Fprintf(w, "\nbest: %s, IoU before rounding %.5f\n", best.Start, best.IoU)

	rg, aff := logofit.Normalise(best.P)
	shape := rg.Shape()
	rounded := logofit.IoU(mask, logofit.Render(shape, aff, mask.W, mask.H, logofit.SS))
	aff3, refit := logofit.RefitAffine(shape, mask, aff)
	r3 := logofit.Render(shape, aff3, mask.W, mask.H, logofit.SS)
	ch3 := logofit.Chamfer(mask, r3)
	h3 := logofit.IoU(mask2, logofit.Render(shape, aff3.Double(), mask2.W, mask2.H, logofit.SS))
	fmt.Fprintf(w, "after normalise + round to 0.25 (same affine): IoU %.5f\n", rounded)
	fmt.Fprintf(w, "after rounding, affine refitted (scale %.4f, offset %.2f,%.2f): IoU %.5f chamfer %.3f px\n", aff3.Sx, aff3.Ox, aff3.Oy, refit, ch3)
	fmt.Fprintf(w, "ceiling (v3 vs its own binarisation): %.4f\n", logofit.IoU(binarise(r3), r3))
	fmt.Fprintf(w, "held-out (original x2 bilinear, re-binarised, same affine doubled): v3 %.4f, v2 %.4f\n", h3, h2)

	if err := os.WriteFile(o.svgOut, []byte(rg.SVG()), 0o644); err != nil {
		return err
	}
	// the file was just written from rg.SVG() (round trip held by
	// TestGeoSVGRoundTrip) and is read back to score what is on disk.
	back, _ := logofit.LoadSVGStrokes(o.svgOut)
	fmt.Fprintf(w, "written %s; reloaded from file: IoU %.5f\n", o.svgOut, logofit.IoU(mask, logofit.Render(back, aff3, mask.W, mask.H, logofit.SS)))
	d := filepath.Join(o.out, "diff-v3.png")
	if err := logofit.WriteDiff(d, mask, back, aff3); err != nil {
		return err
	}
	fmt.Fprintf(w, "diff image %s\n\n", d)

	// v2 placed on the original, expressed in the v3 frame
	fmt.Fprintf(w, "| var | v2 (own grid) | v2 in v3 frame | v3 | delta |\n|---|---|---|---|---|\n")
	for i, name := range logofit.GeoNames {
		var eq float64
		switch {
		case i == logofit.W:
			eq = f2.Sx * logofit.V2Geo[i] / aff3.Sx
		case i <= logofit.XR:
			eq = (f2.Aff.Sx*logofit.V2Geo[i] + f2.Aff.Ox - aff3.Ox) / aff3.Sx
		default:
			eq = (f2.Aff.Sy*logofit.V2Geo[i] + f2.Aff.Oy - aff3.Oy) / aff3.Sy
		}
		fmt.Fprintf(w, "| %s | %g | %.2f | %g | %+.2f |\n", name, logofit.V2Geo[i], eq, rg.G[i], rg.G[i]-eq)
	}
	return nil
}
