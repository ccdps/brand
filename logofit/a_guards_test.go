package logofit

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
)

// This file is named a_*_test.go so that it runs first: a guard whose removal
// makes the code panic is then held by an assertion here, and the panic does
// not end the test binary and hide the tests after it.

// noPanic, deferred, turns a panic into a failed assertion.
func noPanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Errorf("panicked: %v", r)
	}
}

func TestAtIsZeroOutsideOnEverySide(t *testing.T) {
	defer noPanic(t)
	c := Cov{W: 3, H: 2, V: []float64{1, 2, 3, 4, 5, 6}}
	for _, p := range [][2]int{{-1, 0}, {-1, 1}, {0, -1}, {3, 0}, {3, 1}, {0, 2}, {2, 2}, {-1, -1}, {100, 100}} {
		if v := c.At(p[0], p[1]); v != 0 {
			t.Errorf("At(%d,%d) = %v, want 0", p[0], p[1], v)
		}
	}
	for p, want := range map[[2]int]float64{{0, 0}: 1, {2, 0}: 3, {0, 1}: 4, {2, 1}: 6} {
		if v := c.At(p[0], p[1]); v != want {
			t.Errorf("At(%d,%d) = %v, want %v", p[0], p[1], v, want)
		}
	}
}

func TestIoUOfTwoEmptyMapsIsZeroNotNaN(t *testing.T) {
	defer noPanic(t)
	if v := IoU(NewCov(2, 2), NewCov(2, 2)); v != 0 {
		t.Errorf("IoU of empty maps = %v", v)
	}
	one := Cov{W: 2, H: 1, V: []float64{1, 0}}
	if v := IoU(one, one); v != 1 {
		t.Errorf("IoU of identical maps = %v", v)
	}
}

func TestLoadLumFailures(t *testing.T) {
	defer noPanic(t)
	d := t.TempDir()
	if _, err := LoadLum(filepath.Join(d, "missing.png")); !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "missing.png") {
		t.Errorf("missing file: %v", err)
	}
	junk := filepath.Join(d, "junk.png")
	if err := os.WriteFile(junk, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLum(junk); err == nil || !strings.Contains(err.Error(), junk) {
		t.Errorf("not an image: %v", err)
	}
}

func TestLoadFontFailures(t *testing.T) {
	defer noPanic(t)
	if _, err := LoadFontBytes("Junk", 700, []byte("not a font")); err == nil || !strings.Contains(err.Error(), "Junk 700") {
		t.Errorf("garbage: %v", err)
	}
	// U+4E2D has no glyph in Go Bold
	if _, err := LoadFontRunes("Bold", 700, gobold.TTF, "C中"); err == nil || !strings.Contains(err.Error(), "no glyph for") {
		t.Errorf("missing glyph: %v", err)
	}
	if _, err := LoadFont("Nope", 700, filepath.Join(t.TempDir(), "nope.ttf")); err == nil || !strings.Contains(err.Error(), "nope.ttf") {
		t.Errorf("missing file: %v", err)
	}
}

// tableOffset finds a table of a TTF by tag.
func tableOffset(t *testing.T, data []byte, tag string) int {
	t.Helper()
	n := int(binary.BigEndian.Uint16(data[4:]))
	for i := 0; i < n; i++ {
		e := data[12+16*i:]
		if string(e[:4]) == tag {
			return int(binary.BigEndian.Uint32(e[8:]))
		}
	}
	t.Fatalf("no %s table", tag)
	return 0
}

// A font whose glyph data lies outside the file parses, and fails on the glyph.
func TestLoadFontGlyphOutsideTheFile(t *testing.T) {
	defer noPanic(t)
	f, err := LoadFontBytes("Bold", 700, gobold.TTF)
	if err != nil {
		t.Fatal(err)
	}
	g := int(f.gid['C'])
	data := append([]byte(nil), gobold.TTF...)
	loca, head := tableOffset(t, data, "loca"), tableOffset(t, data, "head")
	if binary.BigEndian.Uint16(data[head+50:]) == 0 { // short offsets, halved
		binary.BigEndian.PutUint16(data[loca+2*g:], 0)
		binary.BigEndian.PutUint16(data[loca+2*g+2:], 0xffff)
	} else {
		binary.BigEndian.PutUint32(data[loca+4*g:], 0)
		binary.BigEndian.PutUint32(data[loca+4*g+4:], 0x7fffffff)
	}
	_, err = LoadFontBytes("Bold", 700, data)
	if err == nil || !strings.Contains(err.Error(), `glyph 'C'`) {
		t.Errorf("err = %v, want one naming glyph 'C'", err)
	}
}

func TestSVGStrokesFailures(t *testing.T) {
	defer noPanic(t)
	if _, err := LoadSVGStrokes(filepath.Join(t.TempDir(), "nope.svg")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	for name, c := range map[string]struct{ src, want string }{
		"no stroke-width": {`<svg><path d="M0 0 L5 5"/></svg>`, "no stroke-width"},
		"truncated path":  {`<svg stroke-width="2"><path d="M0"/></svg>`, "truncated path"},
		"unsupported":     {`<svg stroke-width="2"><path d="M0 0 l5 5"/></svg>`, "unsupported path command"},
		"single point":    {`<svg stroke-width="2"><path d="M0 0"/></svg>`, "no paths"},
		"no path at all":  {`<svg stroke-width="2"></svg>`, "no paths"},
		"one segment":     {`<svg stroke-width="2"><path d="M0 0 L5 5"/></svg>`, ""},
	} {
		_, err := ParseSVGStrokes(c.src, "x.svg")
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "x.svg")):
			t.Errorf("%s: err = %v, want one naming x.svg and %q", name, err, c.want)
		}
	}
}

func TestFitShapeOfAnEmptyTarget(t *testing.T) {
	defer noPanic(t)
	f, _ := LoadFontBytes("Bold", 700, gobold.TTF)
	r := FitShape(f.Run("D"), NewCov(10, 10), Uniform, 0)
	if r.IoU != 0 || !math.IsInf(r.Chamfer, 1) || r.Evals != 0 || r.Sx != 0 {
		t.Errorf("fit to nothing: %+v", r)
	}
}

// The XY search keeps both scales within tol of the uniform one, however
// stretched the target is.
func TestFitShapeXYStaysWithinTolerance(t *testing.T) {
	defer noPanic(t)
	f, _ := LoadFontBytes("Bold", 700, gobold.TTF)
	run := f.Run("D")
	const tol = 0.01
	tgt := Render(run, Affine{Sx: 0.0195, Sy: 0.03, Ox: 8, Oy: 52}, 64, 64, SS)
	u := FitShape(run, tgt, Uniform, 0)
	x := FitShape(run, tgt, XY, tol)
	lo, hi := u.Sx*(1-tol)-1e-9, u.Sx*(1+tol)+1e-9
	if x.Sx < lo || x.Sx > hi || x.Sy < lo || x.Sy > hi {
		t.Errorf("XY scales %.5f, %.5f outside [%.5f, %.5f]", x.Sx, x.Sy, lo, hi)
	}
}

func TestObjectiveRefusesDegenerateParameters(t *testing.T) {
	defer noPanic(t)
	obj := objective(NewCov(30, 30))
	p := Params{G: V2Geo, S: 0.5, Ox: 1, Oy: 1}
	if v := obj(p.vec()); v < 0 {
		t.Errorf("valid parameters refused: %v", v)
	}
	for name, mod := range map[string]func(*Params){
		"stroke width below 1": func(p *Params) { p.G[W] = 0.99 },
		"zero scale":           func(p *Params) { p.S = 0 },
		"negative scale":       func(p *Params) { p.S = -0.5 },
	} {
		q := p
		mod(&q)
		if v := obj(q.vec()); v != -1 {
			t.Errorf("%s: objective = %v, want -1", name, v)
		}
	}
	q := p
	q.G[W] = 1
	if v := obj(q.vec()); v < 0 {
		t.Errorf("stroke width exactly 1 refused: %v", v)
	}
}

// The shape lies right of x=30 at any positive scale, so a mask on the left
// matches only a mirrored (negative-scale) placement: RefitAffine must not
// take it.
func TestRefitAffineKeepsAPositiveScale(t *testing.T) {
	defer noPanic(t)
	s, err := ParseSVGStrokes(`<svg stroke-width="10000"><path d="M0 5000 L10000 5000"/></svg>`, "big.svg")
	if err != nil {
		t.Fatal(err)
	}
	mask := NewCov(30, 30)
	for y := 20; y < 30; y++ {
		for x := 20; x < 30; x++ {
			mask.V[y*30+x] = 1
		}
	}
	a, f := RefitAffine(s, mask, Affine{Sx: 0.003, Sy: 0.003, Ox: 30, Oy: 30})
	if a.Sx <= 0 || a.Sx != a.Sy {
		t.Errorf("refit scale = %v, %v", a.Sx, a.Sy)
	}
	if f != 0 {
		t.Errorf("refit IoU = %v, want 0 (no positive placement reaches the mask)", f)
	}
}

// bars draws a dark tile and n dark letter bars right of it on white.
func bars(n int) Cov {
	const w, h = 200, 50
	c := NewCov(w, h)
	for i := range c.V {
		c.V[i] = 255
	}
	rect := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				c.V[y*w+x] = 0
			}
		}
	}
	rect(2, 2, 42, 48)
	for i := 0; i < n; i++ {
		rect(50+i*20, 10, 56+i*20, 40)
	}
	return c
}

func TestExtractWordmarkWantsFiveLetters(t *testing.T) {
	defer noPanic(t)
	for _, n := range []int{0, 4, 6} {
		if _, err := ExtractWordmark(bars(n)); err == nil || !strings.Contains(err.Error(), fmt.Sprintf("found %d letter", n)) {
			t.Errorf("%d letters: %v", n, err)
		}
	}
	wm, err := ExtractWordmark(bars(5))
	if err != nil || wm.Tile == nil {
		t.Errorf("5 letters: %v %+v", err, wm)
	}
}

func TestExtractStrokesOnATileAtTheImageEdge(t *testing.T) {
	defer noPanic(t)
	// the tile fills the image, touching every edge: the border flood must stop
	// at the image edge instead of indexing outside it
	const n = 20
	c := NewCov(n, n)
	c.V[0] = 255              // a rounded corner: light, reachable from the border, on the image edge
	for y := 8; y < 12; y++ { // a light stroke well inside the dark tile
		for x := 5; x < 15; x++ {
			c.V[y*n+x] = 255
		}
	}
	_, m, err := ExtractStrokes(c)
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, v := range m.V {
		sum += v
	}
	if sum != 40 || m.V[8*n+5] != 1 || m.V[0] != 0 {
		t.Errorf("stroke mask has %v pixels", sum)
	}
}

func TestExtractStrokesWithNoDarkComponent(t *testing.T) {
	defer noPanic(t)
	if _, _, err := ExtractStrokes(NewCov(0, 0)); err == nil || !strings.Contains(err.Error(), "no dark component") {
		t.Errorf("empty image: %v", err)
	}
}

func TestWriteDiff(t *testing.T) {
	defer noPanic(t)
	mask := NewCov(4, 4)
	s, err := ParseSVGStrokes(`<svg stroke-width="1"><path d="M0 0 L3 3"/></svg>`, "s.svg")
	if err != nil {
		t.Fatal(err)
	}
	a := Affine{Sx: 1, Sy: 1}
	if err := WriteDiff(filepath.Join(t.TempDir(), "nodir", "d.png"), mask, s, a); err == nil {
		t.Error("WriteDiff into a missing directory succeeded")
	}
	p := filepath.Join(t.TempDir(), "d.png")
	if err := WriteDiff(p, mask, s, a); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil || img.Bounds() != image.Rect(0, 0, 32, 32) {
		t.Errorf("diff image: %v", err)
	}
}

// ---- fetch ----------------------------------------------------------------

func tableDir(tags ...string) []byte {
	b := make([]byte, 12+16*len(tags))
	b[5] = byte(len(tags))
	for i, tg := range tags {
		copy(b[12+16*i:], tg)
	}
	return b
}

func TestHasFvar(t *testing.T) {
	defer noPanic(t)
	for name, c := range map[string]struct {
		data []byte
		want bool
	}{
		"empty":      {nil, false},
		"3 bytes":    {[]byte{1, 2, 3}, false},
		"5 bytes":    {[]byte{0, 0, 0, 0, 9}, false},
		"fvar first": {tableDir("fvar"), true},
		"fvar last":  {tableDir("glyf", "head", "fvar"), true},
		"no fvar":    {tableDir("glyf", "head"), false},
	} {
		if got := hasFvar(c.data); got != c.want {
			t.Errorf("%s: hasFvar = %v, want %v", name, got, c.want)
		}
	}
	over := tableDir("glyf")
	over[5] = 200 // a declared count past the end of the data is not trusted
	if hasFvar(over) {
		t.Error("count past the end found an fvar")
	}
}

func TestGetFailures(t *testing.T) {
	defer noPanic(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			io.WriteString(w, "hello")
		case "/404":
			http.NotFound(w, r)
		case "/cut": // promises 100 bytes, sends 5, hangs up
			w.Header().Set("Content-Length", "100")
			io.WriteString(w, "short")
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
	}))
	c := srv.Client()
	if b, err := get(c, srv.URL+"/ok"); err != nil || string(b) != "hello" {
		t.Errorf("ok: %q %v", b, err)
	}
	if _, err := get(c, srv.URL+"/404"); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("404: %v", err)
	}
	if _, err := get(c, srv.URL+"/cut"); err == nil {
		t.Error("a body cut short was accepted")
	}
	if _, err := get(c, "://bad"); err == nil {
		t.Error("a malformed URL was accepted")
	}
	srv.Close()
	if _, err := get(c, srv.URL+"/ok"); err == nil {
		t.Error("a closed server answered")
	}
}

// fontServer serves a CSS endpoint at /css2 and font files at /f/. Montserrat
// gets a table directory with an fvar table; Poppins a CSS error; Lexend a CSS
// with no font URL; Outfit a missing font file.
func fontServer(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/css2" {
			fam := r.URL.Query().Get("family")
			switch {
			case strings.HasPrefix(fam, "Poppins"):
				http.Error(w, "boom", 500)
			case strings.HasPrefix(fam, "Lexend"):
				io.WriteString(w, "/* latin */ @font-face { font-family: x; }")
			default:
				name := strings.ReplaceAll(strings.ReplaceAll(fam, ":wght@", "-"), " ", "")
				fmt.Fprintf(w, "/* cyrillic */ @font-face { src: url(%s/f/cyr-%s.ttf); }\n/* latin */ @font-face { src: url(%s/f/%s.ttf); }", srv.URL, name, srv.URL, name)
			}
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/f/")
		switch {
		case strings.HasPrefix(name, "cyr-"):
			http.Error(w, "the latin face is the one wanted", 500)
		case strings.HasPrefix(name, "Outfit"):
			http.NotFound(w, r)
		case strings.HasPrefix(name, "Montserrat"):
			w.Write(tableDir("glyf", "fvar"))
		default:
			w.Write(tableDir("glyf"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchFromWritesFilesAndSources(t *testing.T) {
	defer noPanic(t)
	srv := fontServer(t)
	dir := filepath.Join(t.TempDir(), "fonts")
	var log bytes.Buffer
	if err := fetchFrom(srv.Client(), srv.URL+"/css2", dir, &log); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Montserrat-700.ttf", "Figtree-900.ttf", "SOURCES.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
	for _, name := range []string{"Poppins-700.ttf", "Lexend-800.ttf", "Outfit-900.ttf"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s written although its fetch failed", name)
		}
	}
	src, _ := os.ReadFile(filepath.Join(dir, "SOURCES.md"))
	for _, want := range []string{
		"| Montserrat-700.ttf | Montserrat | 700 | " + srv.URL + "/f/Montserrat-700.ttf | `",
		"## Not fetched",
		"- Poppins 700: css HTTP 500",
		"- Lexend 800: no font URL in css",
		"- Outfit 900: HTTP 404",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("SOURCES.md lacks %q", want)
		}
	}
	if n := strings.Count(string(src), "| true |"); n != 3 {
		t.Errorf("%d fvar fonts, want 3 (Montserrat x 3 weights)", n)
	}
	if n := strings.Count(string(src), "| false |"); n != len(Families)*len(Weights)-3-9 {
		t.Errorf("%d non-fvar fonts", n)
	}
	for _, want := range []string{"ok   Figtree-700.ttf (", "FAIL Poppins 700: css HTTP 500"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q", want)
		}
	}
}

type countingRT struct {
	next http.RoundTripper
	n    int
}

func (c *countingRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n++
	return c.next.RoundTrip(r)
}

func TestFetchFromReportsFileSystemFailures(t *testing.T) {
	defer noPanic(t)
	srv := fontServer(t)
	root := t.TempDir()
	notDir := filepath.Join(root, "file")
	if err := os.WriteFile(notDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rt := &countingRT{next: srv.Client().Transport}
	if err := fetchFrom(&http.Client{Transport: rt}, srv.URL+"/css2", filepath.Join(notDir, "fonts"), io.Discard); err == nil {
		t.Error("a directory under a file was created")
	}
	if rt.n != 0 {
		t.Errorf("%d requests made although the directory could not be made", rt.n)
	}
	// the first font's file name is taken by a directory
	dir := filepath.Join(root, "fonts")
	if err := os.MkdirAll(filepath.Join(dir, "Montserrat-700.ttf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fetchFrom(srv.Client(), srv.URL+"/css2", dir, io.Discard); err == nil || !strings.Contains(err.Error(), "Montserrat-700.ttf") {
		t.Errorf("an unwritable font file: %v", err)
	}
}
