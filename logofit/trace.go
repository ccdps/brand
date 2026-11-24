package logofit

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
)

// Geo is the pictogram as 19 named free variables; the 7 stroked paths of
// ccdps-mark-v2.svg (same topology, horizontals horizontal, verticals
// vertical, shared points shared) are derived from it.
type Geo [19]float64

// Indices into Geo.
const (
	XL   = iota // x of the left outer vertical
	XA          // x of the narrow tower's left edge
	XB          // x of the narrow tower's right edge
	XBL         // x of the back building's left stub
	XP          // x of the peak vertical
	XC          // x of the back building's right vertical (and start of the right roof)
	XR1         // x of the right building's inner vertical
	XR          // x of the right outer vertical
	YB          // y of the base line (and of every vertical's bottom)
	YL          // y of the left roof start (on x=XL)
	YA          // y of the tower roof (XA..XB)
	YBLT        // y of the top of the back building's left stub... its start
	YBL         // y where the stub meets the roof rise
	YP          // y of the peak
	YC          // y of the roof vertex at XC
	YR1A        // y where the right roof starts on XC
	YR1         // y of the right building's inner roof vertex
	YR2         // y of the right roof end on XR
	W           // the shared stroke width
)

// GeoNames are the variable names in index order.
var GeoNames = [19]string{"xL", "xA", "xB", "xBL", "xP", "xC", "xR1", "xR", "yBase", "yL", "yA", "yBLtop", "yBL", "yPeak", "yC", "yR1a", "yR1", "yR2", "width"}

// V2Geo is ccdps-mark-v2.svg in its own 256 grid (the +9 translate applied).
var V2Geo = Geo{45, 88, 112, 61, 131, 161, 175, 206, 194, 120, 102, 106, 89, 54, 70, 97, 88, 103, 7}

// baseEnds are the x ends of the base line: flush with the outer strokes'
// outer edges unless set explicitly (after rounding).
type baseEnds struct {
	set  bool
	l, r float64
}

func (g Geo) lines(be baseEnds) [][]pt2 {
	bl, br := g[XL]-g[W]/2, g[XR]+g[W]/2
	if be.set {
		bl, br = be.l, be.r
	}
	yb := g[YB]
	return [][]pt2{
		{{bl, yb}, {br, yb}},
		{{g[XL], yb}, {g[XL], g[YL]}, {g[XA], g[YA]}, {g[XB], g[YA]}, {g[XB], yb}},
		{{g[XA], g[YA]}, {g[XA], yb}},
		{{g[XBL], g[YBLT]}, {g[XBL], g[YBL]}, {g[XP], g[YP]}, {g[XC], g[YC]}, {g[XC], yb}},
		{{g[XP], g[YP]}, {g[XP], yb}},
		{{g[XC], g[YR1A]}, {g[XR1], g[YR1]}, {g[XR], g[YR2]}, {g[XR], yb}},
		{{g[XR1], g[YR1]}, {g[XR1], yb}},
	}
}

// Shape returns the stroked shape.
func (g Geo) Shape() *Strokes { return buildStrokes(g.lines(baseEnds{}), g[W]) }

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// RoundedGeo is a Geo whose values are on the 0.25 grid, with explicit
// base-line ends.
type RoundedGeo struct {
	G    Geo
	L, R float64
}

// Shape returns the stroked shape.
func (r RoundedGeo) Shape() *Strokes {
	return buildStrokes(r.G.lines(baseEnds{true, r.L, r.R}), r.G[W])
}

// SVG writes the 256x256 tile with the glyph, as ccdps-mark-v2.svg does.
func (r RoundedGeo) SVG() string {
	g := r.G
	n := num
	var b strings.Builder
	b.WriteString("<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 256 256\" width=\"512\" height=\"512\">\n")
	b.WriteString("  <rect width=\"256\" height=\"256\" rx=\"20\" fill=\"#141414\"/>\n")
	fmt.Fprintf(&b, "  <g fill=\"none\" stroke=\"#fff\" stroke-width=\"%s\" stroke-linejoin=\"miter\">\n", n(g[W]))
	for _, d := range []string{
		fmt.Sprintf("M%s %s H%s", n(r.L), n(g[YB]), n(r.R)),
		fmt.Sprintf("M%s %s V%s L%s %s H%s V%s", n(g[XL]), n(g[YB]), n(g[YL]), n(g[XA]), n(g[YA]), n(g[XB]), n(g[YB])),
		fmt.Sprintf("M%s %s V%s", n(g[XA]), n(g[YA]), n(g[YB])),
		fmt.Sprintf("M%s %s V%s L%s %s L%s %s V%s", n(g[XBL]), n(g[YBLT]), n(g[YBL]), n(g[XP]), n(g[YP]), n(g[XC]), n(g[YC]), n(g[YB])),
		fmt.Sprintf("M%s %s V%s", n(g[XP]), n(g[YP]), n(g[YB])),
		fmt.Sprintf("M%s %s L%s %s L%s %s V%s", n(g[XC]), n(g[YR1A]), n(g[XR1]), n(g[YR1]), n(g[XR]), n(g[YR2]), n(g[YB])),
		fmt.Sprintf("M%s %s V%s", n(g[XR1]), n(g[YR1]), n(g[YB])),
	} {
		fmt.Fprintf(&b, "    <path d=\"%s\"/>\n", d)
	}
	b.WriteString("  </g>\n</svg>\n")
	return b.String()
}

// Params is Geo plus the global uniform affine px = S*x + Ox, py = S*y + Oy.
type Params struct {
	G         Geo
	S, Ox, Oy float64
}

func (p Params) vec() []float64 {
	v := append([]float64(nil), p.G[:]...)
	return append(v, p.S, p.Ox, p.Oy)
}

func paramsOf(v []float64) Params {
	var p Params
	copy(p.G[:], v[:19])
	p.S, p.Ox, p.Oy = v[19], v[20], v[21]
	return p
}

// Aff is the pixel affine.
func (p Params) Aff() Affine { return Affine{p.S, p.S, p.Ox, p.Oy} }

func objective(mask Cov) func([]float64) float64 {
	return func(v []float64) float64 {
		p := paramsOf(v)
		if p.G[W] < 1 || p.S <= 0 {
			return -1
		}
		return IoU(mask, Render(p.G.Shape(), p.Aff(), mask.W, mask.H, SS))
	}
}

// TraceResult is one optimiser run.
type TraceResult struct {
	P     Params
	IoU   float64
	Start string
}

// Trace maximises soft IoU over Geo and the global affine by compass search
// from start, then from `jitters` perturbed starts (run in parallel); the
// best is returned along with every run.
func Trace(mask Cov, start Params, jitters int) (TraceResult, []TraceResult) {
	obj := objective(mask)
	run := func(p Params, name string) TraceResult {
		steps := make([]float64, 22)
		mins := make([]float64, 22)
		for i := 0; i < 19; i++ {
			steps[i], mins[i] = 1, 0.01
		}
		steps[W], mins[W] = 0.5, 0.01
		steps[19], mins[19] = 0.004, 0.00005
		steps[20], steps[21], mins[20], mins[21] = 0.25, 0.25, 0.005, 0.005
		evals := 0
		v, f := patternSearch(p.vec(), steps, mins, obj, &evals)
		return TraceResult{paramsOf(v), f, name}
	}
	all := make([]TraceResult, jitters+1)
	var wg sync.WaitGroup
	for k := 0; k <= jitters; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := start
			name := "v2 start"
			if k > 0 {
				rng := rand.New(rand.NewSource(int64(k)))
				for i := 0; i < 18; i++ {
					p.G[i] += (rng.Float64()*2 - 1) * 2
				}
				p.G[W] += (rng.Float64()*2 - 1) * 0.5
				p.Ox += (rng.Float64()*2 - 1) * 0.5
				p.Oy += (rng.Float64()*2 - 1) * 0.5
				name = fmt.Sprintf("jitter %d", k)
			}
			all[k] = run(p, name)
		}()
	}
	wg.Wait()
	best := all[0]
	for _, r := range all {
		if r.IoU > best.IoU {
			best = r
		}
	}
	return best, all
}

// RefScale is the pixels-per-unit convention of the final 256 grid.
const RefScale = 0.5

func round4(x float64) float64 { return math.Round(x*4) / 4 }

// Normalise maps a fit onto the 256 grid: coordinates scaled so that
// RefScale px per unit reproduces the fit, the glyph (strokes included)
// centred in the tile, everything rounded to 0.25. It returns the rounded
// geometry and the pixel affine that goes with it at the exact (unrounded)
// positions, to be refitted.
func Normalise(p Params) (RoundedGeo, Affine) {
	k := p.S / RefScale
	var g Geo
	for i := 0; i < 19; i++ {
		g[i] = p.G[i] * k
	}
	x0, y0, x1, y1 := g.Shape().Bounds()
	tx, ty := round4(128-(x0+x1)/2), round4(128-(y0+y1)/2)
	var r RoundedGeo
	for i := 0; i < 19; i++ {
		switch {
		case i == W:
			r.G[i] = round4(g[i])
		case i <= XR:
			r.G[i] = round4(g[i] + tx)
		default:
			r.G[i] = round4(g[i] + ty)
		}
	}
	r.L, r.R = round4(r.G[XL]-r.G[W]/2), round4(r.G[XR]+r.G[W]/2)
	return r, Affine{RefScale, RefScale, p.Ox - RefScale*tx, p.Oy - RefScale*ty}
}

// RefitAffine searches the best uniform scale and offset for a fixed shape.
func RefitAffine(s Shape, mask Cov, a Affine) (Affine, float64) {
	obj := func(v []float64) float64 {
		if v[0] <= 0 {
			return -1
		}
		return IoU(mask, Render(s, Affine{v[0], v[0], v[1], v[2]}, mask.W, mask.H, SS))
	}
	evals := 0
	v, f := patternSearch([]float64{a.Sx, a.Ox, a.Oy}, []float64{0.004, 0.25, 0.25}, []float64{0.00005, 0.005, 0.005}, obj, &evals)
	return Affine{v[0], v[0], v[1], v[2]}, f
}

// Upscale2 doubles c with bilinear interpolation (pixel centres aligned),
// so original pixel p maps to 2p..2p+2 in the result.
func Upscale2(c Cov) Cov {
	o := NewCov(c.W*2, c.H*2)
	for y := 0; y < o.H; y++ {
		fy := (float64(y)+0.5)/2 - 0.5
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		for x := 0; x < o.W; x++ {
			fx := (float64(x)+0.5)/2 - 0.5
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			at := func(xx, yy int) float64 {
				return c.V[min(max(yy, 0), c.H-1)*c.W+min(max(xx, 0), c.W-1)]
			}
			o.V[y*o.W+x] = (1-ty)*((1-tx)*at(x0, y0)+tx*at(x0+1, y0)) + ty*((1-tx)*at(x0, y0+1)+tx*at(x0+1, y0+1))
		}
	}
	return o
}

// Double returns the affine of the x2 image.
func (a Affine) Double() Affine { return Affine{2 * a.Sx, 2 * a.Sy, 2 * a.Ox, 2 * a.Oy} }
