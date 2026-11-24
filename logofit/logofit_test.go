package logofit

import (
	"math"
	"os"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func row(v ...float64) Cov { return Cov{W: len(v), H: 1, V: v} }

func TestIoU(t *testing.T) {
	cases := []struct {
		name string
		a, b Cov
		want float64
	}{
		{"identical", row(0, 1, 1, 0), row(0, 1, 1, 0), 1},
		{"disjoint", row(1, 0, 0, 0), row(0, 0, 0, 1), 0},
		{"one third", row(1, 1, 0, 0), row(0, 1, 1, 0), 1.0 / 3},
		{"soft", row(1, 0.5), row(0.5, 0.5), (0.5 + 0.5) / (1 + 0.5)},
		{"empty", row(0, 0), row(0, 0), 0},
	}
	for _, c := range cases {
		if got := IoU(c.a, c.b); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("%s: IoU = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestChamfer(t *testing.T) {
	px := func(w, h int, pts ...[2]int) Cov {
		c := NewCov(w, h)
		for _, p := range pts {
			c.V[p[1]*w+p[0]] = 1
		}
		return c
	}
	if got := Chamfer(px(8, 3, [2]int{1, 1}), px(8, 3, [2]int{4, 1})); got != 3 {
		t.Errorf("single pixels 3 apart: chamfer = %v, want 3", got)
	}
	a := px(8, 3, [2]int{1, 1})
	if got := Chamfer(a, a); got != 0 {
		t.Errorf("identical: chamfer = %v, want 0", got)
	}
	// asymmetric: A = {0, 4}, B = {0}: A->B mean (0+4)/2 = 2, B->A = 0, symmetric = 1
	if got := Chamfer(px(8, 1, [2]int{0, 0}, [2]int{4, 0}), px(8, 1, [2]int{0, 0})); got != 1 {
		t.Errorf("asymmetric: chamfer = %v, want 1", got)
	}
	if got := Chamfer(NewCov(3, 3), a); !math.IsInf(got, 1) {
		t.Errorf("empty edge set: chamfer = %v, want +Inf", got)
	}
	// edges of a 3x3 block: the 8 border pixels, not the centre
	blk := NewCov(5, 5)
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			blk.V[y*5+x] = 1
		}
	}
	if n := len(Edges(blk, 0.5)); n != 8 {
		t.Errorf("3x3 block has %d edge pixels, want 8", n)
	}
}

func TestOtsuSeparatesTwoLevels(t *testing.T) {
	c := row(10, 10, 10, 10, 240, 240, 240, 240)
	if th := Otsu(c); th < 10 || th >= 240 {
		t.Errorf("Otsu = %d, want in [10,240)", th)
	}
}

func TestPositiveControl(t *testing.T) {
	bold, err := LoadFontBytes("GoBold", 700, gobold.TTF)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := LoadFontBytes("GoRegular", 400, goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	const size, ox, oy = 56.0, 160.37, 71.61
	wm, err := ExtractWordmark(SynthLockup(bold, 451, 138, size, ox, oy))
	if err != nil {
		t.Fatal(err)
	}
	self, other := Score(wm, bold), Score(wm, reg)
	// ceiling: the target is binarised by Otsu, the render stays soft
	if self.MeanIoU < 0.92 || self.WordIoU < 0.92 {
		t.Errorf("self fit too low: mean %.4f word %.4f", self.MeanIoU, self.WordIoU)
	}
	if other.MeanIoU > self.MeanIoU-0.05 || other.WordIoU > self.WordIoU-0.05 {
		t.Errorf("another font scores too close: self %.4f/%.4f other %.4f/%.4f",
			self.MeanIoU, self.WordIoU, other.MeanIoU, other.WordIoU)
	}
	if math.Abs(self.WordSize-size) > 0.2 || math.Abs(self.WordOx-ox) > 0.3 || math.Abs(self.WordOy-oy) > 0.3 {
		t.Errorf("did not recover the known placement: size %.3f origin (%.3f,%.3f)", self.WordSize, self.WordOx, self.WordOy)
	}
	if self.Chamfer > 0.2 {
		t.Errorf("self chamfer %.3f px", self.Chamfer)
	}
}

func TestGeoSVGRoundTrip(t *testing.T) {
	g := V2Geo
	r := RoundedGeo{G: g, L: 41.5, R: 209.5}
	p := t.TempDir() + "/g.svg"
	if err := os.WriteFile(p, []byte(r.SVG()), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSVGStrokes(p)
	if err != nil {
		t.Fatal(err)
	}
	ax0, ay0, ax1, ay1 := r.Shape().Bounds()
	bx0, by0, bx1, by1 := back.Bounds()
	for i, d := range []float64{ax0 - bx0, ay0 - by0, ax1 - bx1, ay1 - by1} {
		if math.Abs(d) > 1e-9 {
			t.Errorf("bounds differ by %v at %d", d, i)
		}
	}
	// the stroked area of the written SVG equals the in-memory shape
	a := Render(r.Shape(), Affine{1, 1, 0, 0}, 256, 256, SS)
	b := Render(back, Affine{1, 1, 0, 0}, 256, 256, SS)
	if iou := IoU(a, b); iou < 0.9999 {
		t.Errorf("round trip IoU %v", iou)
	}
}

func TestUpscale2KeepsConstant(t *testing.T) {
	c := NewCov(3, 3)
	for i := range c.V {
		c.V[i] = 7
	}
	for _, v := range Upscale2(c).V {
		if v != 7 {
			t.Fatalf("got %v", v)
		}
	}
}

func TestStrokesRenderWidth(t *testing.T) {
	// a horizontal 7-unit stroke of length 20 at scale 1 covers 140 px
	s := buildStrokes([][]pt2{{{2, 5}, {22, 5}}}, 7)
	c := Render(s, Affine{1, 1, 0, 0}, 30, 12, SS)
	var sum float64
	for _, v := range c.V {
		sum += v
	}
	if math.Abs(sum-140) > 0.5 {
		t.Errorf("stroke area %.2f, want 140", sum)
	}
}
