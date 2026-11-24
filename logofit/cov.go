// Package logofit measures how well a font, or a vector redraw, matches a
// raster logo: coverage masks, IoU, chamfer distance and a pattern search.
package logofit

import (
	"fmt"
	"image"
	_ "image/png" // decoder for the PNG inputs
	"math"
	"os"
	"sort"
)

// Cov is a W x H map of coverage values in [0,1]; a binary mask is a Cov
// holding only 0 and 1.
type Cov struct {
	W, H int
	V    []float64
}

// NewCov allocates a zero Cov.
func NewCov(w, h int) Cov { return Cov{W: w, H: h, V: make([]float64, w*h)} }

// At returns the value at (x,y), 0 outside.
func (c Cov) At(x, y int) float64 {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return 0
	}
	return c.V[y*c.W+x]
}

// IoU is the (soft) intersection over union, sum(min)/sum(max). For binary
// inputs it is the ordinary IoU. Two empty maps give 0.
func IoU(a, b Cov) float64 {
	if a.W != b.W || a.H != b.H {
		panic("logofit: IoU size mismatch")
	}
	var in, un float64
	for i, x := range a.V {
		y := b.V[i]
		if x < y {
			in += x
			un += y
		} else {
			in += y
			un += x
		}
	}
	if un == 0 {
		return 0
	}
	return in / un
}

// Edges lists the pixels at or above thr that have a 4-neighbour below thr
// (or outside the image).
func Edges(c Cov, thr float64) [][2]int {
	var out [][2]int
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if c.V[y*c.W+x] < thr {
				continue
			}
			if c.At(x-1, y) < thr || c.At(x+1, y) < thr || c.At(x, y-1) < thr || c.At(x, y+1) < thr {
				out = append(out, [2]int{x, y})
			}
		}
	}
	return out
}

func meanMinDist(a, b [][2]int) float64 {
	var sum float64
	for _, p := range a {
		best := math.MaxFloat64
		for _, q := range b {
			dx, dy := float64(p[0]-q[0]), float64(p[1]-q[1])
			if d := dx*dx + dy*dy; d < best {
				best = d
			}
		}
		sum += math.Sqrt(best)
	}
	return sum / float64(len(a))
}

// Chamfer is the symmetric chamfer distance, in pixels, between the edge
// sets of a and b (each thresholded at 0.5): the mean of the two directed
// mean nearest-neighbour distances. +Inf when either edge set is empty.
func Chamfer(a, b Cov) float64 {
	ea, eb := Edges(a, 0.5), Edges(b, 0.5)
	if len(ea) == 0 || len(eb) == 0 {
		return math.Inf(1)
	}
	return (meanMinDist(ea, eb) + meanMinDist(eb, ea)) / 2
}

// Crop returns the w x h window of c whose top-left is (x0,y0).
func Crop(c Cov, x0, y0, w, h int) Cov {
	o := NewCov(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o.V[y*w+x] = c.At(x0+x, y0+y)
		}
	}
	return o
}

// LoadLum reads a PNG, composites it over white and returns the luminance
// (0..255, Rec. 601 weights).
func LoadLum(path string) (Cov, error) {
	f, err := os.Open(path)
	if err != nil {
		return Cov{}, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return Cov{}, fmt.Errorf("%s: %w", path, err)
	}
	b := img.Bounds()
	c := NewCov(b.Dx(), b.Dy())
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA() // premultiplied, 16 bit
			inv := float64(0xffff - a)
			lum := 0.299*(float64(r)+inv) + 0.587*(float64(g)+inv) + 0.114*(float64(bl)+inv)
			c.V[y*c.W+x] = lum / 257
		}
	}
	return c, nil
}

// Otsu returns the threshold t maximising the between-class variance of the
// luminance histogram; "dark" is lum <= t.
func Otsu(lum Cov) int {
	var hist [256]float64
	for _, v := range lum.V {
		i := int(math.Round(v))
		if i < 0 {
			i = 0
		}
		if i > 255 {
			i = 255
		}
		hist[i]++
	}
	total := float64(len(lum.V))
	var sumAll float64
	for i, h := range hist {
		sumAll += float64(i) * h
	}
	var wB, sumB, bestVar float64
	best := 0
	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += float64(t) * hist[t]
		mB, mF := sumB/wB, (sumAll-sumB)/wF
		if v := wB * wF * (mB - mF) * (mB - mF); v > bestVar {
			bestVar, best = v, t
		}
	}
	return best
}

// Dark returns the binary mask lum <= t.
func Dark(lum Cov, t int) Cov {
	m := NewCov(lum.W, lum.H)
	for i, v := range lum.V {
		if v <= float64(t) {
			m.V[i] = 1
		}
	}
	return m
}

// Comp is an 8-connected component of a binary mask.
type Comp struct {
	Area                   int
	MinX, MinY, MaxX, MaxY int // inclusive
	Pix                    []int
}

// Width and Height are the inclusive bounding-box sizes.
func (c Comp) Width() int  { return c.MaxX - c.MinX + 1 }
func (c Comp) Height() int { return c.MaxY - c.MinY + 1 }

// Mask returns the component as a w x h binary Cov.
func (c Comp) Mask(w, h int) Cov {
	m := NewCov(w, h)
	for _, p := range c.Pix {
		m.V[p] = 1
	}
	return m
}

// Components labels the 8-connected components of m (values >= 0.5).
func Components(m Cov) []Comp {
	seen := make([]bool, len(m.V))
	var out []Comp
	for s := range m.V {
		if seen[s] || m.V[s] < 0.5 {
			continue
		}
		c := Comp{MinX: m.W, MinY: m.H, MaxX: -1, MaxY: -1}
		stack := []int{s}
		seen[s] = true
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := p%m.W, p/m.W
			c.Pix = append(c.Pix, p)
			c.Area++
			c.MinX, c.MaxX = min(c.MinX, x), max(c.MaxX, x)
			c.MinY, c.MaxY = min(c.MinY, y), max(c.MaxY, y)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= m.W || ny >= m.H {
						continue
					}
					q := ny*m.W + nx
					if !seen[q] && m.V[q] >= 0.5 {
						seen[q] = true
						stack = append(stack, q)
					}
				}
			}
		}
		sort.Ints(c.Pix)
		out = append(out, c)
	}
	return out
}

// bbox returns the inclusive bounding box of the pixels >= 0.5.
func bbox(c Cov) (x0, y0, x1, y1 int, ok bool) {
	x0, y0, x1, y1 = c.W, c.H, -1, -1
	for y := 0; y < c.H; y++ {
		for x := 0; x < c.W; x++ {
			if c.V[y*c.W+x] >= 0.5 {
				x0, x1 = min(x0, x), max(x1, x)
				y0, y1 = min(y0, y), max(y1, y)
			}
		}
	}
	return x0, y0, x1, y1, x1 >= 0
}
