package brandkit

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccdps/brand/logofit"
)

func newKit(t *testing.T) *Kit {
	t.Helper()
	svg, err := os.ReadFile("../assets/ccdps-mark-v3.svg")
	if err != nil {
		t.Fatal(err)
	}
	ttf, err := os.ReadFile("../fonts/Figtree-700.ttf")
	if err != nil {
		t.Fatal(err)
	}
	k, err := New(string(svg), ttf)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestGenerateThenCheck(t *testing.T) {
	k := newKit(t)
	dir := t.TempDir()
	names, err := k.Generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 25 {
		t.Fatalf("only %d files", len(names))
	}
	if bad := k.Check(dir, ""); len(bad) > 0 {
		t.Fatal(bad)
	}
	other := t.TempDir()
	if _, err := k.Generate(other); err != nil {
		t.Fatal(err)
	}
	if bad := k.Check(dir, other); len(bad) > 0 {
		t.Fatal("two generations differ:", bad)
	}
}

func TestCheckFailsOnDamage(t *testing.T) {
	k := newKit(t)
	dir := t.TempDir()
	if _, err := k.Generate(dir); err != nil {
		t.Fatal(err)
	}
	for name, damage := range map[string]func(string) error{
		"missing file": func(d string) error { return os.Remove(filepath.Join(d, "mark-64.png")) },
		"text in svg": func(d string) error {
			return os.WriteFile(filepath.Join(d, "mark-colour.svg"), []byte("<svg><text>x</text></svg>"), 0o644)
		},
		"wrong size": func(d string) error { return os.WriteFile(filepath.Join(d, "mark-16.png"), mustPNG(t, k, 17), 0o644) },
		"bad ico": func(d string) error {
			return os.WriteFile(filepath.Join(d, "favicon.ico"), []byte{0, 0, 1, 0, 3, 0}, 0o644)
		},
		"oversize file": func(d string) error {
			return os.WriteFile(filepath.Join(d, "tokens.json"), append([]byte("{}"), make([]byte, MaxFileBytes)...), 0o644)
		},
	} {
		d := t.TempDir()
		copyDir(t, dir, d)
		if err := damage(d); err != nil {
			t.Fatal(err)
		}
		if bad := k.Check(d, ""); len(bad) == 0 {
			t.Errorf("%s: Check passed", name)
		}
	}
	// pixel comparison catches a changed PNG
	d := t.TempDir()
	copyDir(t, dir, d)
	if err := os.WriteFile(filepath.Join(d, "mark-64.png"), mustPNG(t, k, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	if bad := k.Check(d, dir); len(bad) == 0 {
		t.Error("Check --against passed on a different mark-64.png")
	}
}

func mustPNG(t *testing.T, k *Kit, n int) []byte {
	t.Helper()
	sc, err := k.Mark(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := VariantByName("white")
	b, err := EncodePNG(sc.Raster(v, Canvas{W: n, H: n, Scale: float64(n) / TileSize}))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	es, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The raster of the colour mark must agree with an independent rendering of
// the same SVG paths by logofit (the one that measured 0.974 IoU on the original).
func TestMarkRasterMatchesStrokeRender(t *testing.T) {
	k := newKit(t)
	sc, _ := k.Mark(TileRadius, StrokeWidth)
	v, _ := VariantByName("colour")
	img := sc.Raster(v, Canvas{W: 256, H: 256, Scale: 1, Supersamp: 4})
	want := logofit.Render(sc.Strokes, logofit.Affine{Sx: 1, Sy: 1}, 256, 256, 4)
	// strokes are white on the dark tile: luminance ~ coverage inside the tile
	got := logofit.NewCov(256, 256)
	for i := range got.V {
		got.V[i] = float64(img.Pix[i*4]) / 255
	}
	// compare only pixels well inside the tile (the corners are transparent)
	var num, den float64
	for y := 24; y < 232; y++ {
		for x := 24; x < 232; x++ {
			g, w := got.V[y*256+x], want.V[y*256+x]
			num += math.Abs(g - (0x12/255.0)*(1-w) - w)
			den++
		}
	}
	if m := num / den; m > 0.002 {
		t.Errorf("mean abs difference %.5f", m)
	}
}

func TestKnockoutCutsTheStrokes(t *testing.T) {
	k := newKit(t)
	sc, _ := k.Mark(TileRadius, StrokeWidth)
	v, _ := VariantByName("white")
	img := sc.Raster(v, Canvas{W: 256, H: 256, Scale: 1})
	// (47,150) is on the left vertical stroke (x 47, width 8): transparent;
	// (20,150) is inside the tile beside it: opaque white
	if a := img.NRGBAAt(47, 150).A; a != 0 {
		t.Errorf("stroke pixel alpha %d, want 0", a)
	}
	if p := img.NRGBAAt(20, 150); p.A != 255 || p.R != 255 {
		t.Errorf("tile pixel %v, want opaque white", p)
	}
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("corner alpha %d, want 0 (rounded tile)", a)
	}
}

func TestSVGHasNoTextAndOutlinesTheWordmark(t *testing.T) {
	k := newKit(t)
	l, err := k.Lockup(StrokeWidth)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range Variants {
		s := l.SVG(v)
		if strings.Contains(s, "<text") || !strings.Contains(s, `<path fill="`) {
			t.Errorf("%s: no wordmark outline or live text", v.Name)
		}
	}
	if l.W < 700 || l.W > 1000 {
		t.Errorf("lockup width %.1f outside the expected 700..1000", l.W)
	}
	// the wordmark must clear the tile by the measured gap (less the bearing)
	if l.WordAff.Ox != TileSize+WordGap {
		t.Errorf("wordmark origin %v", l.WordAff.Ox)
	}
}

func TestBaselineIsJustifiedToTheWordmarkAndPlacedAsMeasured(t *testing.T) {
	k := newKit(t)
	l, err := k.Lockup(StrokeWidth)
	if err != nil {
		t.Fatal(err)
	}
	wx0, _, wx1, _ := l.Word.Bounds()
	bx0, by0, bx1, by1 := l.Base.Bounds()
	wl, wr := l.WordAff.Ox+l.WordAff.Sx*wx0, l.WordAff.Ox+l.WordAff.Sx*wx1
	bl, br := l.BaseAff.Ox+l.BaseAff.Sx*bx0, l.BaseAff.Ox+l.BaseAff.Sx*bx1
	if math.Abs(wl-bl) > 0.01 || math.Abs(wr-br) > 0.01 {
		t.Errorf("baseline ink %.2f..%.2f, wordmark ink %.2f..%.2f", bl, br, wl, wr)
	}
	if got, want := l.BaseAff.Oy+l.BaseAff.Sy*by1, baseBottomY; math.Abs(got-want) > 0.01 {
		t.Errorf("baseline bottom %.2f, want %.2f", got, want)
	}
	if h := l.BaseAff.Sy * (by1 - by0); math.Abs(h-baseInkHeight) > 0.01 {
		t.Errorf("baseline ink height %.2f, want %.2f", h, baseInkHeight)
	}
	if l.WordAff.Oy != letterBaselineY {
		t.Errorf("wordmark baseline %v, want %v", l.WordAff.Oy, letterBaselineY)
	}
	if l.BaseTrack <= 0 || l.BaseTrack > 0.5 || l.CapUnits <= 0 {
		t.Errorf("tracking %.4f em, cap %.2f", l.BaseTrack, l.CapUnits)
	}
	for _, v := range Variants {
		if strings.Count(l.SVG(v), `<path fill="`) != 2 {
			t.Errorf("%s: want wordmark and baseline paths", v.Name)
		}
	}
	m, _ := k.Mark(TileRadius, StrokeWidth)
	if m.Base != nil || strings.Count(m.SVG(Variants[0]), `<path fill="`) != 0 {
		t.Error("pictogram carries text")
	}
}

func TestICOStructure(t *testing.T) {
	b, err := ICO([][]byte{{1, 2, 3}, {4, 5}}, []int{16, 256})
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(b[4:]) != 2 || b[6] != 16 || b[22] != 0 {
		t.Errorf("header %v", b[:38])
	}
	if off := binary.LittleEndian.Uint32(b[6+12:]); off != 38 || !bytes.Equal(b[38:41], []byte{1, 2, 3}) {
		t.Errorf("first offset %d", off)
	}
	if _, err := ICO(nil, nil); err == nil {
		t.Error("empty ico accepted")
	}
	if _, err := ICO([][]byte{{1}}, []int{300}); err == nil {
		t.Error("size 300 accepted")
	}
}

func TestPNGsAreOpaqueWhereRequired(t *testing.T) {
	k := newKit(t)
	dir := t.TempDir()
	if _, err := k.Generate(dir); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"apple-touch-icon.png", "avatar-512.png", "social-banner-1280x640.png"} {
		f, _ := os.Open(filepath.Join(dir, n))
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		b := img.Bounds()
		for _, p := range [][2]int{{0, 0}, {b.Dx() - 1, 0}, {0, b.Dy() - 1}, {b.Dx() - 1, b.Dy() - 1}} {
			if _, _, _, a := img.At(p[0], p[1]).RGBA(); a != 0xffff {
				t.Errorf("%s corner %v is not opaque", n, p)
			}
		}
	}
}

func TestNewRejectsBadSources(t *testing.T) {
	ttf, _ := os.ReadFile("../fonts/Figtree-700.ttf")
	if _, err := New("<svg/>", ttf); err == nil {
		t.Error("svg without stroke-width accepted")
	}
	if _, err := New(`<svg stroke-width="8"/>`, ttf); err == nil {
		t.Error("svg without paths accepted")
	}
	if _, err := New(`<svg stroke-width="8"><path d="M0 0 H1"/></svg>`, []byte("not a font")); err == nil {
		t.Error("garbage font accepted")
	}
}
