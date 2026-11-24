package brandkit

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrokeForCutsAtSmallMaxPx(t *testing.T) {
	for px, want := range map[int]float64{16: SmallStroke, SmallMaxPx: SmallStroke, SmallMaxPx + 1: StrokeWidth, 1024: StrokeWidth} {
		if got := strokeFor(px); got != want {
			t.Errorf("strokeFor(%d) = %v, want %v", px, got, want)
		}
	}
}

// noPanic, deferred, turns a panic into a failed assertion: a guard whose
// removal crashes the code is then held by a test that says so, and the
// crash no longer ends the test binary and hides the tests after it.
func noPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Errorf("panicked: %v", r)
	}
}

func TestNewRejectsAnUnsupportedPathCommand(t *testing.T) {
	defer noPanic(t)
	ttf, err := os.ReadFile("../fonts/Figtree-700.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(`<svg stroke-width="8"><path d="M0 0 l1 1"/></svg>`, ttf); err == nil {
		t.Error("a path with a relative command was accepted")
	}
}

func TestVariantByName(t *testing.T) {
	defer noPanic(t)
	for _, v := range Variants {
		got, ok := VariantByName(v.Name)
		if !ok || got != v {
			t.Errorf("%s: got %v, %v", v.Name, got, ok)
		}
	}
	if v, ok := VariantByName("nope"); ok || v != (Variant{}) {
		t.Errorf("unknown variant: %v, %v", v, ok)
	}
}

func TestMarkPanicsOnAnUnpreparedStroke(t *testing.T) {
	k := newKit(t)
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "no stroke cut") {
			t.Errorf("recover = %v", r)
		}
	}()
	k.Mark(0, 3)
}

func TestGenerateReportsWriteFailures(t *testing.T) {
	k := newKit(t)
	// the directory cannot be made: its path is a file
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Generate(filepath.Join(f, "sub")); err == nil {
		t.Error("Generate into a path under a file succeeded")
	}
	// an entry cannot be written: a directory has its name
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, "mark-colour.svg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Generate(d); err == nil {
		t.Error("Generate over a directory named like an entry succeeded")
	}
}

func icoOf(t *testing.T, k *Kit, pngs [][]byte, sizes []int) []byte {
	t.Helper()
	b, err := ICO(pngs, sizes)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCheckICOReportsEachDefect(t *testing.T) {
	defer noPanic(t)
	k := newKit(t)
	e := Entry{Name: "x.ico", Kind: "ico", ICO: []int{16}}
	good := icoOf(t, k, [][]byte{mustPNG(t, k, 16)}, []int{16})
	if bad := checkICO(e, good); len(bad) != 0 {
		t.Fatalf("a good ico was refused: %v", bad)
	}
	cases := map[string]struct {
		data []byte
		want string
	}{
		"too short":       {[]byte{0, 0, 1, 0, 1}, "bad ICO header"},
		"not an icon":     {[]byte{0, 0, 2, 0, 1, 0}, "bad ICO header"},
		"wrong count":     {[]byte{0, 0, 1, 0, 2, 0}, "bad ICO header"},
		"truncated dir":   {append([]byte{0, 0, 1, 0, 1, 0}, make([]byte, 10)...), "truncated directory"},
		"out of range":    {good[:len(good)-1], "out of range"},
		"undecodable":     {icoOf(t, k, [][]byte{{1, 2, 3}}, []int{16}), "entry 0"},
		"wrong dimension": {icoOf(t, k, [][]byte{mustPNG(t, k, 17)}, []int{16}), "want 16"},
	}
	for name, c := range cases {
		bad := checkICO(e, c.data)
		if len(bad) != 1 || !strings.Contains(bad[0], c.want) {
			t.Errorf("%s: %v, want one message containing %q", name, bad, c.want)
		}
	}
}

func TestCompareReportsEachDifference(t *testing.T) {
	defer noPanic(t)
	k := newKit(t)
	dir := t.TempDir()
	other := filepath.Join(dir, "other")
	write := func(b []byte) {
		t.Helper()
		if err := os.WriteFile(other, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	svg := Entry{Name: "a.svg", Kind: "svg"}
	if bad := compare(svg, []byte("x"), filepath.Join(dir, "missing")); len(bad) != 1 || !strings.Contains(bad[0], "a.svg") || strings.Contains(bad[0], "differs") {
		t.Errorf("missing file: %v", bad)
	}
	write([]byte("y"))
	if bad := compare(svg, []byte("x"), other); len(bad) != 1 || !strings.Contains(bad[0], "differs from") {
		t.Errorf("differing svg: %v", bad)
	}
	write([]byte("x"))
	if bad := compare(svg, []byte("x"), other); bad != nil {
		t.Errorf("equal svg: %v", bad)
	}
	json := Entry{Name: "t.json", Kind: "json"}
	write([]byte("{}"))
	if bad := compare(json, []byte("{ }"), other); len(bad) != 1 {
		t.Errorf("differing json: %v", bad)
	}

	ico := Entry{Name: "x.ico", Kind: "ico", ICO: []int{16}}
	a := icoOf(t, k, [][]byte{mustPNG(t, k, 16)}, []int{16})
	b := icoOf(t, k, [][]byte{mustPNG(t, k, 17)}, []int{16})
	write(a)
	if bad := compare(ico, a, other); bad != nil {
		t.Errorf("equal ico: %v", bad)
	}
	if bad := compare(ico, b, other); len(bad) != 1 || !strings.Contains(bad[0], "x.ico[0]") {
		t.Errorf("differing ico entry: %v", bad)
	}
	short := a[:20]
	if bad := compare(ico, short, other); len(bad) != 1 || !strings.Contains(bad[0], "truncated") {
		t.Errorf("short data: %v", bad)
	}
	write(short)
	if bad := compare(ico, a, other); len(bad) != 1 || !strings.Contains(bad[0], "truncated") {
		t.Errorf("short other: %v", bad)
	}
}

func encodeNRGBA(t *testing.T, w, h int, px color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, px)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestPixelDiff(t *testing.T) {
	defer noPanic(t)
	base := encodeNRGBA(t, 2, 2, color.NRGBA{10, 20, 30, 255})
	if bad := pixelDiff("p", base, base); bad != nil {
		t.Errorf("identical: %v", bad)
	}
	near := encodeNRGBA(t, 2, 2, color.NRGBA{10 + MaxPixelDiff, 20, 30, 255})
	if bad := pixelDiff("p", base, near); bad != nil {
		t.Errorf("difference at the bound: %v", bad)
	}
	if bad := pixelDiff("p", near, base); bad != nil {
		t.Errorf("negative difference at the bound: %v", bad)
	}
	far := encodeNRGBA(t, 2, 2, color.NRGBA{10 + MaxPixelDiff + 1, 20, 30, 255})
	if bad := pixelDiff("p", base, far); len(bad) != 1 || !strings.Contains(bad[0], "worst channel difference") {
		t.Errorf("difference past the bound: %v", bad)
	}
	if bad := pixelDiff("p", far, base); len(bad) != 1 {
		t.Errorf("negative difference past the bound: %v", bad)
	}
	if bad := pixelDiff("p", base, encodeNRGBA(t, 3, 2, color.NRGBA{10, 20, 30, 255})); len(bad) != 1 || !strings.Contains(bad[0], "sizes differ") {
		t.Errorf("different sizes: %v", bad)
	}
	if bad := pixelDiff("p", []byte("junk"), base); len(bad) != 1 || !strings.HasPrefix(bad[0], "p: ") {
		t.Errorf("corrupt first: %v", bad)
	}
	if bad := pixelDiff("p", base, []byte("junk")); len(bad) != 1 || !strings.HasPrefix(bad[0], "p: ") {
		t.Errorf("corrupt second: %v", bad)
	}
}

// A paletted PNG decodes to a non-NRGBA image; it must be compared by colour.
func TestPixelDiffConvertsOtherImageTypes(t *testing.T) {
	pal := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.NRGBA{10, 20, 30, 255}, color.NRGBA{200, 20, 30, 255}})
	var b bytes.Buffer
	if err := png.Encode(&b, pal); err != nil {
		t.Fatal(err)
	}
	if bad := pixelDiff("p", b.Bytes(), encodeNRGBA(t, 2, 2, color.NRGBA{10, 20, 30, 255})); bad != nil {
		t.Errorf("same colours: %v", bad)
	}
	if bad := pixelDiff("p", b.Bytes(), encodeNRGBA(t, 2, 2, color.NRGBA{200, 20, 30, 255})); len(bad) != 1 {
		t.Errorf("different colours: %v", bad)
	}
}
