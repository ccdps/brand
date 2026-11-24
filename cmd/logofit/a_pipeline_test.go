package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gobold"

	"github.com/ccdps/brand/logofit"
)

// noPanic, deferred, turns a panic into a failed assertion: a guard whose
// removal crashes the code is then held by a test that says so, and the crash
// does not end the test binary and hide the tests after it.
func noPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Errorf("panicked: %v", r)
	}
}

// fx is a directory of synthetic stand-ins for the originals (which are not in
// the repository): a lockup, a pictogram and a font directory.
type fx struct {
	dir, lockup, mark, fonts, out string
}

func grayPNG(t *testing.T, path string, c logofit.Cov) {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, c.W, c.H))
	for i, v := range c.V {
		img.Pix[i] = uint8(v + 0.5)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func white(w, h int) logofit.Cov {
	c := logofit.NewCov(w, h)
	for i := range c.V {
		c.V[i] = 255
	}
	return c
}

func newFx(t *testing.T) fx {
	t.Helper()
	d := t.TempDir()
	x := fx{dir: d, lockup: filepath.Join(d, "lockup.png"), mark: filepath.Join(d, "mark.png"),
		fonts: filepath.Join(d, "fonts"), out: filepath.Join(d, "out")}
	bold, err := logofit.LoadFontBytes("Montserrat", 800, gobold.TTF)
	if err != nil {
		t.Fatal(err)
	}
	grayPNG(t, x.lockup, logofit.SynthLockup(bold, 451, 138, 56, 160.37, 71.61))

	// pictogram: the v3 strokes, white on a dark tile, on white paper
	s, err := logofit.LoadSVGStrokes("../../assets/ccdps-mark-v3.svg")
	if err != nil {
		t.Fatal(err)
	}
	cov := logofit.Render(s, logofit.Affine{Sx: 0.5, Sy: 0.5, Ox: 4, Oy: 4}, 136, 136, logofit.SS)
	m := white(136, 136)
	for y := 4; y < 132; y++ {
		for x := 4; x < 132; x++ {
			m.V[y*136+x] = 20 + 235*cov.V[y*136+x]
		}
	}
	grayPNG(t, x.mark, m)

	if err := os.Mkdir(x.fonts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x.fonts, logofit.FontFile("Montserrat", 800)), gobold.TTF, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x.fonts, logofit.FontFile("Inter", 700)), []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x.dir, "bold.ttf"), gobold.TTF, 0o644); err != nil {
		t.Fatal(err)
	}
	return x
}

func cli(args ...string) (string, string, error) {
	var out, errb bytes.Buffer
	c := RootCmd()
	c.SetOut(&out)
	c.SetErr(&errb)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), errb.String(), err
}

func (x fx) runArgs(extra ...string) []string {
	return append([]string{"run", "--lockup", x.lockup, "--mark", x.mark, "--fonts", x.fonts, "--out", x.out,
		"--svg", "v2=../../testdata/ccdps-mark-v2.svg", "--workers", "2"}, extra...)
}

func (x fx) mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRunEndToEnd(t *testing.T) {
	defer noPanic(t)
	x := newFx(t)
	out, _, err := cli(x.runArgs()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# logofit report", "tile: x ", "letters: rows ", "glyph 4 window origin",
		"missing font " + filepath.Join(x.fonts, "Poppins-700.ttf"), "unusable font " + filepath.Join(x.fonts, "Inter-700.ttf"),
		"Ranking (1 fonts", "| 1 | Montserrat | 800 |", "Positive control", "Montserrat-800 rendered at em 56.00",
		"self: rank 1,", "worst font Montserrat 800", "## Pictogram", "| v2 | uniform |", "| v2 | xy +-30% |",
		"ceiling (redraw vs its own binarisation)", "| diff image | " + filepath.Join(x.out, "diff-v2.png")} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	rep, err := os.ReadFile(filepath.Join(x.out, "report.md"))
	if err != nil || string(rep) != out {
		t.Errorf("report.md differs from stdout (%v)", err)
	}
	csv, err := os.ReadFile(filepath.Join(x.out, "ranking.csv"))
	if err != nil || !strings.HasPrefix(string(csv), "family,weight,") || !strings.Contains(string(csv), "\nMontserrat,800,") {
		t.Errorf("ranking.csv: %v %q", err, csv)
	}
	if _, err := os.Stat(filepath.Join(x.out, "diff-v2.png")); err != nil {
		t.Error(err)
	}
}

func TestRunFailures(t *testing.T) {
	defer noPanic(t)
	blank := func(t *testing.T, p string) { grayPNG(t, p, white(60, 60)) }
	cases := []struct {
		name  string
		setup func(t *testing.T, x fx) []string // extra args
		want  string
	}{
		{"out under a file", func(t *testing.T, x fx) []string {
			os.WriteFile(filepath.Join(x.dir, "f"), nil, 0o644)
			return []string{"--out", filepath.Join(x.dir, "f", "o")}
		}, "not a directory"},
		{"report is a directory", func(t *testing.T, x fx) []string { x.mkdir(t, filepath.Join(x.out, "report.md")); return nil }, "report.md"},
		{"lockup missing", func(t *testing.T, x fx) []string { return []string{"--lockup", "/nonexistent-lockup.png"} }, "/nonexistent-lockup.png"},
		{"lockup blank", func(t *testing.T, x fx) []string { blank(t, x.lockup); return nil }, "letter components"},
		{"no fonts", func(t *testing.T, x fx) []string { return []string{"--fonts", x.dir} }, "no fonts in"},
		{"ranking.csv is a directory", func(t *testing.T, x fx) []string { x.mkdir(t, filepath.Join(x.out, "ranking.csv")); return nil }, "ranking.csv"},
		{"control not loaded", func(t *testing.T, x fx) []string { return []string{"--control", "Nope-1"} }, "control font Nope-1 not loaded"},
		{"mark missing", func(t *testing.T, x fx) []string { return []string{"--mark", "/nonexistent-mark.png"} }, "/nonexistent-mark.png"},
		{"mark blank", func(t *testing.T, x fx) []string { blank(t, x.mark); return nil }, "no dark component"},
		{"svg without name", func(t *testing.T, x fx) []string { return []string{"--svg", "justapath"} }, "want name=path"},
		{"svg missing", func(t *testing.T, x fx) []string { return []string{"--svg", "v=/nonexistent.svg"} }, "/nonexistent.svg"},
		{"diff is a directory", func(t *testing.T, x fx) []string { x.mkdir(t, filepath.Join(x.out, "diff-v2.png")); return nil }, "diff-v2.png"},
	}
	for _, c := range cases {
		x := newFx(t)
		extra := c.setup(t, x)
		// a later flag overrides an earlier one
		_, _, err := cli(x.runArgs(extra...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one containing %q", c.name, err, c.want)
		}
	}
}

func TestMeasureEndToEnd(t *testing.T) {
	defer noPanic(t)
	x := newFx(t)
	out, _, err := cli("measure", "--lockup", x.lockup, "--font", filepath.Join(x.dir, "bold.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"image 451x138, Otsu ", "tile: x ", "letters: rows ", "baseline text: x ", "letter C: em ",
		"letter S: em ", "mean em ", "implied tracking ", "first pen origin to tile right edge",
		"tile colour (median of opaque pixels): #141414", "wordmark colour (median of opaque pixels): #000000"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

func TestMeasureFailures(t *testing.T) {
	defer noPanic(t)
	x := newFx(t)
	font := filepath.Join(x.dir, "bold.ttf")
	if _, _, err := cli("measure", "--lockup", "/nonexistent-lockup.png", "--font", font); err == nil || !strings.Contains(err.Error(), "/nonexistent-lockup.png") {
		t.Errorf("missing lockup: %v", err)
	}
	if _, _, err := cli("measure", "--lockup", x.lockup, "--font", "/nonexistent.ttf"); err == nil || !strings.Contains(err.Error(), "/nonexistent.ttf") {
		t.Errorf("missing font: %v", err)
	}
	blank := filepath.Join(x.dir, "blank.png")
	grayPNG(t, blank, white(60, 60))
	if _, _, err := cli("measure", "--lockup", blank, "--font", font); err == nil || !strings.Contains(err.Error(), "letter components") {
		t.Errorf("blank lockup: %v", err)
	}
	// letters but no tile: refused, not dereferenced
	bold, _ := logofit.LoadFontBytes("Montserrat", 800, gobold.TTF)
	lum := logofit.SynthLockup(bold, 451, 138, 56, 160.37, 71.61)
	for y := 8; y < 130; y++ {
		for xx := 8; xx < 136; xx++ {
			lum.V[y*451+xx] = 255
		}
	}
	noTile := filepath.Join(x.dir, "notile.png")
	grayPNG(t, noTile, lum)
	if _, _, err := cli("measure", "--lockup", noTile, "--font", font); err == nil || !strings.Contains(err.Error(), "no tile") {
		t.Errorf("lockup without a tile: %v", err)
	}
}

func TestMedianColour(t *testing.T) {
	defer noPanic(t)
	d := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	img.SetNRGBA(0, 0, color.NRGBA{10, 100, 200, 255})
	img.SetNRGBA(1, 0, color.NRGBA{30, 110, 220, 255})
	img.SetNRGBA(2, 0, color.NRGBA{20, 120, 210, 0}) // transparent: ignored
	p := filepath.Join(d, "i.png")
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	mask := logofit.Cov{W: 3, H: 1, V: []float64{1, 1, 1}}
	r, g, bl, err := medianColour(p, mask)
	if err != nil || r != 30 || g != 110 || bl != 220 { // two samples: the upper median
		t.Errorf("median = %d %d %d, %v", r, g, bl, err)
	}
	// a mask cell below 0.5 is not sampled
	r, g, bl, err = medianColour(p, logofit.Cov{W: 3, H: 1, V: []float64{1, 0.4, 1}})
	if err != nil || r != 10 || g != 100 || bl != 200 {
		t.Errorf("median under a partial mask = %d %d %d, %v", r, g, bl, err)
	}
	if _, _, _, err := medianColour(p, logofit.Cov{W: 3, H: 1, V: []float64{0, 0, 1}}); err == nil || !strings.Contains(err.Error(), "no opaque pixels") {
		t.Errorf("only a transparent pixel under the mask: %v", err)
	}
	if _, _, _, err := medianColour(filepath.Join(d, "missing.png"), mask); err == nil || !strings.Contains(err.Error(), "missing.png") {
		t.Errorf("missing file: %v", err)
	}
	junk := filepath.Join(d, "junk.png")
	if err := os.WriteFile(junk, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := medianColour(junk, mask); err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("not an image: %v", err)
	}
}

func TestBinarise(t *testing.T) {
	defer noPanic(t)
	b := binarise(logofit.Cov{W: 4, H: 1, V: []float64{0, 0.49, 0.5, 1}})
	for i, want := range []float64{0, 0, 1, 1} {
		if b.V[i] != want {
			t.Errorf("binarise[%d] = %v, want %v", i, b.V[i], want)
		}
	}
}

func (x fx) traceArgs(extra ...string) []string {
	return append([]string{"trace", "--mark", x.mark, "--v2", "../../testdata/ccdps-mark-v2.svg", "--out", x.out,
		"--svg-out", filepath.Join(x.dir, "v3.svg"), "--jitters", "0"}, extra...)
}

func TestTraceEndToEnd(t *testing.T) {
	defer noPanic(t)
	x := newFx(t)
	out, _, err := cli(x.traceArgs()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# v3 trace", "run v2 start", "best: v2 start", "after normalise + round to 0.25",
		"ceiling (v3 vs its own binarisation)", "held-out", "written " + filepath.Join(x.dir, "v3.svg"),
		"diff image " + filepath.Join(x.out, "diff-v3.png"), "| var | v2 (own grid) |"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if rows := strings.Count(out, "\n| ") - 1; rows != len(logofit.GeoNames) { // minus the header
		t.Errorf("%d table rows, want %d", rows, len(logofit.GeoNames))
	}
	if rep, err := os.ReadFile(filepath.Join(x.out, "trace-report.md")); err != nil || string(rep) != out {
		t.Errorf("trace-report.md differs from stdout (%v)", err)
	}
	svg, err := os.ReadFile(filepath.Join(x.dir, "v3.svg"))
	if err != nil || !strings.Contains(string(svg), "<svg") {
		t.Errorf("v3.svg: %v", err)
	}
}

func TestTraceFailures(t *testing.T) {
	defer noPanic(t)
	cases := []struct {
		name  string
		setup func(t *testing.T, x fx) []string
		want  string
	}{
		{"out under a file", func(t *testing.T, x fx) []string {
			os.WriteFile(filepath.Join(x.dir, "f"), nil, 0o644)
			return []string{"--out", filepath.Join(x.dir, "f", "o")}
		}, "not a directory"},
		{"report is a directory", func(t *testing.T, x fx) []string { x.mkdir(t, filepath.Join(x.out, "trace-report.md")); return nil }, "trace-report.md"},
		{"mark missing", func(t *testing.T, x fx) []string { return []string{"--mark", "/nonexistent-mark.png"} }, "/nonexistent-mark.png"},
		{"mark blank", func(t *testing.T, x fx) []string { grayPNG(t, x.mark, white(60, 60)); return nil }, "no dark component"},
		{"v2 missing", func(t *testing.T, x fx) []string { return []string{"--v2", "/nonexistent-v2.svg"} }, "/nonexistent-v2.svg"},
		{"svg-out unwritable", func(t *testing.T, x fx) []string {
			return []string{"--svg-out", filepath.Join(x.dir, "nodir", "v3.svg")}
		}, "nodir"},
		{"diff is a directory", func(t *testing.T, x fx) []string { x.mkdir(t, filepath.Join(x.out, "diff-v3.png")); return nil }, "diff-v3.png"},
	}
	for _, c := range cases {
		x := newFx(t)
		extra := c.setup(t, x)
		_, _, err := cli(x.traceArgs(extra...)...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one containing %q", c.name, err, c.want)
		}
	}
}
