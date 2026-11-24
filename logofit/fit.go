package logofit

import (
	"math"
)

// Mode selects the scale freedom of a fit.
type Mode int

const (
	// Uniform fits one scale plus a sub-pixel offset.
	Uniform Mode = iota
	// XY fits independent x and y scales (within a tolerance of the uniform
	// scale) plus a sub-pixel offset.
	XY
)

// SS is the supersampling factor used for every rendering.
const SS = 4

// Fit is the best placement of a shape against a target.
type Fit struct {
	Aff     Affine  // window coordinates
	Sx, Sy  float64 // pixels per shape unit
	Dx, Dy  float64 // offset from bounding-box-centre alignment, pixels
	IoU     float64
	Chamfer float64
	Evals   int
}

// patternSearch maximises f by compass search with step halving.
func patternSearch(x, steps, minSteps []float64, f func([]float64) float64, evals *int) ([]float64, float64) {
	best := f(x)
	*evals++
	for iter := 0; iter < 4000; iter++ {
		improved := false
	coords:
		for i := range x {
			for _, sg := range []float64{1, -1} {
				y := append([]float64(nil), x...)
				y[i] += sg * steps[i]
				v := f(y)
				*evals++
				if v > best+1e-12 {
					x, best, improved = y, v, true
					break coords
				}
			}
		}
		if improved {
			continue
		}
		done := true
		for i := range steps {
			steps[i] /= 2
			if steps[i] > minSteps[i] {
				done = false
			}
		}
		if done {
			break
		}
	}
	return x, best
}

// FitShape places s over tgt (a window-sized Cov) to maximise IoU. Start:
// scale so the bounding-box heights match, centres aligned; a coarse scale
// grid (+-15%), then compass search on (scale, dx, dy); for XY mode a second
// search on (sx, sy, dx, dy) with sx, sy kept within +-tol of the uniform
// scale. Offsets are relative to bounding-box-centre alignment.
func FitShape(s Shape, tgt Cov, mode Mode, tol float64) Fit {
	tx0, ty0, tx1, ty1, ok := bbox(tgt)
	if !ok {
		return Fit{IoU: 0, Chamfer: math.Inf(1)}
	}
	tcx, tcy := float64(tx0+tx1+1)/2, float64(ty0+ty1+1)/2
	sx0, sy0, sx1, sy1 := s.Bounds()
	cx, cy := (sx0+sx1)/2, (sy0+sy1)/2
	aff := func(sx, sy, dx, dy float64) Affine {
		return Affine{sx, sy, tcx + dx - cx*sx, tcy + dy - cy*sy}
	}
	evals := 0
	obj := func(sx, sy, dx, dy float64) float64 {
		return IoU(tgt, Render(s, aff(sx, sy, dx, dy), tgt.W, tgt.H, SS))
	}
	s0 := float64(ty1-ty0+1) / (sy1 - sy0)
	bestS, bestV := s0, -1.0
	for k := -6; k <= 6; k++ {
		sc := s0 * (1 + 0.025*float64(k))
		evals++
		if v := obj(sc, sc, 0, 0); v > bestV {
			bestS, bestV = sc, v
		}
	}
	steps := []float64{0.01 * bestS, 0.5, 0.5}
	mins := []float64{0.0002 * bestS, 0.01, 0.01}
	x, _ := patternSearch([]float64{bestS, 0, 0}, steps, mins,
		func(p []float64) float64 { return obj(p[0], p[0], p[1], p[2]) }, &evals)
	su := x[0]
	res := Fit{Sx: su, Sy: su, Dx: x[1], Dy: x[2]}
	if mode == XY {
		lo, hi := su*(1-tol), su*(1+tol)
		steps := []float64{0.005 * su, 0.005 * su, 0.25, 0.25}
		mins := []float64{0.0002 * su, 0.0002 * su, 0.01, 0.01}
		x, _ = patternSearch([]float64{su, su, x[1], x[2]}, steps, mins, func(p []float64) float64 {
			if p[0] < lo || p[0] > hi || p[1] < lo || p[1] > hi {
				return -1
			}
			return obj(p[0], p[1], p[2], p[3])
		}, &evals)
		res = Fit{Sx: x[0], Sy: x[1], Dx: x[2], Dy: x[3]}
	}
	res.Aff = aff(res.Sx, res.Sy, res.Dx, res.Dy)
	r := Render(s, res.Aff, tgt.W, tgt.H, SS)
	res.IoU = IoU(tgt, r)
	res.Chamfer = Chamfer(tgt, r)
	res.Evals = evals
	return res
}
