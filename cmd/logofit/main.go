// Command logofit measures which free font best matches the CCDPS wordmark
// and how closely an SVG redraw matches the pictogram.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ccdps/brand/logofit"
)

var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		osExit(1)
	}
}

// RootCmd builds the command tree.
func RootCmd() *cobra.Command {
	root := &cobra.Command{Use: "logofit", Short: "objective logo / font / redraw fit measurements", SilenceUsage: true}
	root.AddCommand(fetchCmd(), runCmd(), traceCmd(), measureCmd())
	return root
}

func fetchCmd() *cobra.Command {
	var dir string
	c := &cobra.Command{Use: "fetch", Short: "download the candidate fonts and write fonts/SOURCES.md",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return logofit.Fetch(dir, cmd.OutOrStdout()) }}
	c.Flags().StringVar(&dir, "fonts", "fonts", "font directory")
	return c
}

type runOpts struct {
	lockup, mark, fonts, out string
	svgs                     []string
	workers                  int
	control                  string
}

func runCmd() *cobra.Command {
	var o runOpts
	c := &cobra.Command{Use: "run", Short: "wordmark ranking, positive control and pictogram fit", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return run(o, cmd.OutOrStdout()) }}
	f := c.Flags()
	f.StringVar(&o.lockup, "lockup", "", "original lockup PNG (read only; required, not in the repository)")
	f.StringVar(&o.mark, "mark", "", "original pictogram PNG (read only; required, not in the repository)")
	_ = c.MarkFlagRequired("lockup")
	_ = c.MarkFlagRequired("mark")
	f.StringSliceVar(&o.svgs, "svg", []string{"v1=testdata/ccdps-mark-v1.svg", "v2=testdata/ccdps-mark-v2.svg"}, "name=path SVG redraws")
	f.StringVar(&o.fonts, "fonts", "fonts", "font directory (from `logofit fetch`)")
	f.StringVar(&o.out, "out", "out", "output directory")
	f.IntVar(&o.workers, "workers", min(4, runtime.NumCPU()), "parallel workers")
	f.StringVar(&o.control, "control", "Montserrat-800", "font used for the positive control")
	return c
}

func loadFonts(dir string, log io.Writer) ([]*logofit.Font, error) {
	var fonts []*logofit.Font
	for _, fam := range logofit.Families {
		for _, w := range logofit.Weights {
			p := filepath.Join(dir, logofit.FontFile(fam, w))
			if _, err := os.Stat(p); err != nil {
				fmt.Fprintf(log, "missing font %s\n", p)
				continue
			}
			f, err := logofit.LoadFont(fam, w, p)
			if err != nil {
				fmt.Fprintf(log, "unusable font %s: %v\n", p, err)
				continue
			}
			fonts = append(fonts, f)
		}
	}
	if len(fonts) == 0 {
		return nil, fmt.Errorf("no fonts in %s (run `logofit fetch`)", dir)
	}
	return fonts, nil
}

func table(w io.Writer, rs []logofit.Result, n int) {
	fmt.Fprintf(w, "| # | font | wt | mean glyph IoU | per-glyph C C D P S | mean IoU (xy +-8%%) | chamfer px | whole-word IoU | word chamfer px | word em px |\n|---|---|---|---|---|---|---|---|---|---|\n")
	for i, r := range rs {
		if i >= n {
			break
		}
		fmt.Fprintf(w, "| %d | %s | %d | %.4f | %.3f %.3f %.3f %.3f %.3f | %.4f | %.3f | %.4f | %.3f | %.1f |\n",
			i+1, r.Family, r.Weight, r.MeanIoU, r.GlyphIoU[0], r.GlyphIoU[1], r.GlyphIoU[2], r.GlyphIoU[3], r.GlyphIoU[4],
			r.MeanIoUXY, r.Chamfer, r.WordIoU, r.WordChamf, r.WordSize)
	}
}

func run(o runOpts, stdout io.Writer) error {
	// a failed MkdirAll is reported by the os.Create below, naming the path.
	_ = os.MkdirAll(o.out, 0o755)
	rep, err := os.Create(filepath.Join(o.out, "report.md"))
	if err != nil {
		return err
	}
	defer rep.Close()
	w := io.MultiWriter(stdout, rep)

	// 1. wordmark
	lum, err := logofit.LoadLum(o.lockup)
	if err != nil {
		return err
	}
	wm, err := logofit.ExtractWordmark(lum)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "# logofit report\n\n## Wordmark\n\nimage %dx%d, Otsu threshold %d (dark = lum <= %d)\n", wm.W, wm.H, wm.Threshold, wm.Threshold)
	if wm.Tile != nil {
		fmt.Fprintf(w, "tile: x %d-%d y %d-%d\n", wm.Tile.MinX, wm.Tile.MaxX, wm.Tile.MinY, wm.Tile.MaxY)
	}
	fmt.Fprintf(w, "letters: rows %d-%d; baseline text: %d components rows %d-%d\n", wm.LetterTop, wm.LetterB, wm.SmallComps, wm.SmallTop, wm.SmallBot)
	for i, l := range wm.Letters {
		fmt.Fprintf(w, "  glyph %d window origin (%d,%d) size %dx%d\n", i, l.X0, l.Y0, l.Mask.W, l.Mask.H)
	}

	// 2-4. fonts
	fonts, err := loadFonts(o.fonts, w)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\n## Ranking (%d fonts, by mean per-glyph IoU)\n\n", len(fonts))
	rs := logofit.Rank(wm, fonts, o.workers)
	table(w, rs, len(rs))
	var csv strings.Builder
	csv.WriteString("family,weight,mean_iou,mean_iou_xy,chamfer,chamfer_xy,word_iou,word_chamfer,word_size_px\n")
	for _, r := range rs {
		fmt.Fprintf(&csv, "%s,%d,%.5f,%.5f,%.4f,%.4f,%.5f,%.4f,%.2f\n", r.Family, r.Weight, r.MeanIoU, r.MeanIoUXY, r.Chamfer, r.ChamferXY, r.WordIoU, r.WordChamf, r.WordSize)
	}
	if err := os.WriteFile(filepath.Join(o.out, "ranking.csv"), []byte(csv.String()), 0o644); err != nil {
		return err
	}

	// 6. positive control
	if err := control(o, fonts, w); err != nil {
		return err
	}

	// 5. pictogram
	return pictogram(o, w)
}

func control(o runOpts, fonts []*logofit.Font, w io.Writer) error {
	var cf *logofit.Font
	for _, f := range fonts {
		if strings.ReplaceAll(f.Family, " ", "")+fmt.Sprintf("-%d", f.Weight) == o.control {
			cf = f
		}
	}
	if cf == nil {
		return fmt.Errorf("control font %s not loaded", o.control)
	}
	const size, ox, oy = 56.0, 160.37, 71.61
	lum := logofit.SynthLockup(cf, 451, 138, size, ox, oy)
	// SynthLockup always draws the tile and the five letters CCDPS, so the
	// extraction cannot fail on it (TestRunEndToEnd runs this path).
	wm, _ := logofit.ExtractWordmark(lum)
	fmt.Fprintf(w, "\n## Positive control\n\n%s rendered at em %.2f px, pen origin (%.2f, %.2f), through the same pipeline (Otsu %d).\n\n", o.control, size, ox, oy, wm.Threshold)
	rs := logofit.Rank(wm, fonts, o.workers)
	table(w, rs, 5)
	for i, r := range rs {
		if r.Family == cf.Family && r.Weight == cf.Weight {
			fmt.Fprintf(w, "\nself: rank %d, mean IoU %.4f, whole-word IoU %.4f, recovered em %.3f (truth %.2f), origin (%.3f, %.3f) (truth %.2f, %.2f)\n",
				i+1, r.MeanIoU, r.WordIoU, r.WordSize, size, r.WordOx, r.WordOy, ox, oy)
		}
	}
	for _, name := range []string{"Poppins", "Raleway", "Rubik"} {
		for _, r := range rs {
			if r.Family == name && r.Weight == 800 {
				fmt.Fprintf(w, "other font %s %d: mean IoU %.4f, whole-word IoU %.4f\n", r.Family, r.Weight, r.MeanIoU, r.WordIoU)
			}
		}
	}
	last := rs[len(rs)-1]
	fmt.Fprintf(w, "worst font %s %d: mean IoU %.4f\n", last.Family, last.Weight, last.MeanIoU)
	return nil
}

func pictogram(o runOpts, w io.Writer) error {
	lum, err := logofit.LoadLum(o.mark)
	if err != nil {
		return err
	}
	t, mask, err := logofit.ExtractStrokes(lum)
	if err != nil {
		return err
	}
	var n int
	for _, v := range mask.V {
		if v > 0 {
			n++
		}
	}
	fmt.Fprintf(w, "\n## Pictogram\n\n%dx%d, Otsu threshold %d, stroke pixels %d\n\n", lum.W, lum.H, t, n)
	fmt.Fprintf(w, "| redraw | fit | IoU | chamfer px | sx | sy | dx | dy |\n|---|---|---|---|---|---|---|---|\n")
	for _, spec := range o.svgs {
		name, path, ok := strings.Cut(spec, "=")
		if !ok {
			return fmt.Errorf("--svg %q: want name=path", spec)
		}
		s, err := logofit.LoadSVGStrokes(path)
		if err != nil {
			return err
		}
		for _, m := range []struct {
			label string
			mode  logofit.Mode
		}{{"uniform", logofit.Uniform}, {"xy +-30%", logofit.XY}} {
			f := logofit.FitShape(s, mask, m.mode, 0.30)
			fmt.Fprintf(w, "| %s | %s | %.4f | %.3f | %.4f | %.4f | %.2f | %.2f |\n", name, m.label, f.IoU, f.Chamfer, f.Sx, f.Sy, f.Dx, f.Dy)
			if m.mode == logofit.XY {
				// ceiling: the redraw scored against its own 0.5-binarisation
				// (the original is a binary mask, the render is soft)
				r := logofit.Render(s, f.Aff, mask.W, mask.H, logofit.SS)
				b := logofit.NewCov(r.W, r.H)
				for i, v := range r.V {
					if v >= 0.5 {
						b.V[i] = 1
					}
				}
				fmt.Fprintf(w, "|  | ceiling (redraw vs its own binarisation) | %.4f | | | | | |\n", logofit.IoU(b, r))
				d := filepath.Join(o.out, "diff-"+name+".png")
				if err := logofit.WriteDiff(d, mask, s, f.Aff); err != nil {
					return err
				}
				fmt.Fprintf(w, "|  | diff image | %s | | | | | |\n", d)
			}
		}
	}
	return nil
}
