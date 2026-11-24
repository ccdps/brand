package logofit

import (
	"fmt"
	"image"
	"image/draw"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Affine maps shape units to pixels: X = Ox + Sx*x, Y = Oy + Sy*y (y down).
type Affine struct{ Sx, Sy, Ox, Oy float64 }

// Shape is anything that can be filled by the vector rasteriser.
type Shape interface {
	// Draw adds the shape to z, with every pixel coordinate multiplied by ss.
	Draw(z *vector.Rasterizer, a Affine, ss float64)
	// Bounds is the (control point) bounding box in shape units.
	Bounds() (x0, y0, x1, y1 float64)
}

// Render fills s at 4x (or ss x) supersampling and box-downsamples to w x h.
func Render(s Shape, a Affine, w, h, ss int) Cov {
	z := vector.NewRasterizer(w*ss, h*ss)
	s.Draw(z, a, float64(ss))
	al := image.NewAlpha(image.Rect(0, 0, w*ss, h*ss))
	z.Draw(al, al.Bounds(), image.Opaque, image.Point{})
	c := NewCov(w, h)
	norm := 255 * float64(ss*ss)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum int
			for j := 0; j < ss; j++ {
				row := (y*ss+j)*al.Stride + x*ss
				for i := 0; i < ss; i++ {
					sum += int(al.Pix[row+i])
				}
			}
			c.V[y*w+x] = float64(sum) / norm
		}
	}
	return c
}

// ---- font outlines -------------------------------------------------------

// unitPPEM is the size glyph outlines are loaded at; shape units are
// 1/unitPPEM em, so Affine.Sx = size_px / unitPPEM.
const unitPPEM = 2048

type placed struct {
	segs sfnt.Segments
	x    float64 // pen x in shape units
}

// Font is a parsed font with the outlines of the runes we use.
type Font struct {
	Family string
	Weight int
	f      *sfnt.Font
	segs   map[rune]sfnt.Segments
	adv    map[rune]float64
	gid    map[rune]sfnt.GlyphIndex
}

// LoadFontBytes parses TTF/OTF data, keeping the outlines of C D P S.
func LoadFontBytes(family string, weight int, data []byte) (*Font, error) {
	return LoadFontRunes(family, weight, data, "CDPS")
}

// LoadFontRunes parses TTF/OTF data, keeping the outlines of the given runes.
func LoadFontRunes(family string, weight int, data []byte, runes string) (*Font, error) {
	f, err := sfnt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s %d: %w", family, weight, err)
	}
	ft := &Font{Family: family, Weight: weight, f: f,
		segs: map[rune]sfnt.Segments{}, adv: map[rune]float64{}, gid: map[rune]sfnt.GlyphIndex{}}
	var b sfnt.Buffer
	for _, r := range runes {
		g, err := f.GlyphIndex(&b, r)
		if err != nil || g == 0 {
			return nil, fmt.Errorf("%s %d: no glyph for %q", family, weight, r)
		}
		segs, err := f.LoadGlyph(&b, g, fixed.I(unitPPEM), nil)
		if err != nil {
			return nil, fmt.Errorf("%s %d: glyph %q: %w", family, weight, r, err)
		}
		ft.segs[r] = append(sfnt.Segments(nil), segs...)
		// GlyphAdvance fails only when g is out of range (GlyphIndex returned
		// it, so it is not) or hmtx lies outside the file (sfnt.Parse checked).
		a, _ := f.GlyphAdvance(&b, g, fixed.I(unitPPEM), font.HintingNone)
		ft.adv[r] = float64(a) / 64
		ft.gid[r] = g
	}
	return ft, nil
}

// LoadFont reads a font file.
func LoadFont(family string, weight int, path string) (*Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadFontBytes(family, weight, data)
}

// GlyphRun is a string laid out on one baseline (advance widths plus any
// legacy 'kern' table pairs; GPOS kerning is not read by sfnt).
type GlyphRun struct {
	glyphs []placed
	x0, y0 float64
	x1, y1 float64
}

// Run lays out text (only the loaded runes are available).
func (f *Font) Run(text string) *GlyphRun { return f.RunTracked(text, 0) }

// UnitsPerEm is the number of shape units per em of a GlyphRun.
const UnitsPerEm = unitPPEM

// Advance is the advance width of r in shape units (1/UnitsPerEm em).
func (f *Font) Advance(r rune) float64 { return f.adv[r] }

// RunTracked is Run with `track` extra shape units (1/2048 em) between letters.
func (f *Font) RunTracked(text string, track float64) *GlyphRun {
	r := &GlyphRun{x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
	var pen float64
	var b sfnt.Buffer
	prev := rune(-1)
	for _, c := range text {
		if prev >= 0 {
			if k, err := f.f.Kern(&b, f.gid[prev], f.gid[c], fixed.I(unitPPEM), font.HintingNone); err == nil {
				pen += float64(k) / 64
			}
		}
		segs := f.segs[c]
		r.glyphs = append(r.glyphs, placed{segs, pen})
		for _, s := range segs {
			n := 1
			switch s.Op {
			case sfnt.SegmentOpQuadTo:
				n = 2
			case sfnt.SegmentOpCubeTo:
				n = 3
			}
			for i := 0; i < n; i++ {
				x, y := float64(s.Args[i].X)/64+pen, float64(s.Args[i].Y)/64
				r.x0, r.x1 = math.Min(r.x0, x), math.Max(r.x1, x)
				r.y0, r.y1 = math.Min(r.y0, y), math.Max(r.y1, y)
			}
		}
		pen += f.adv[c] + track
		prev = c
	}
	return r
}

// PathD returns the outlines as an SVG path `d` string (absolute M/L/Q/C/Z),
// transformed by a and rounded to two decimals.
func (r *GlyphRun) PathD(a Affine) string {
	var b strings.Builder
	f := func(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100+0, 'f', -1, 64) }
	pt := func(p fixed.Point26_6, xoff float64) string {
		return f(a.Ox+a.Sx*(float64(p.X)/64+xoff)) + " " + f(a.Oy+a.Sy*float64(p.Y)/64)
	}
	for _, g := range r.glyphs {
		open := false
		for _, s := range g.segs {
			switch s.Op {
			case sfnt.SegmentOpMoveTo:
				if open {
					b.WriteString("Z")
				}
				open = true
				b.WriteString("M" + pt(s.Args[0], g.x))
			case sfnt.SegmentOpLineTo:
				b.WriteString("L" + pt(s.Args[0], g.x))
			case sfnt.SegmentOpQuadTo:
				b.WriteString("Q" + pt(s.Args[0], g.x) + " " + pt(s.Args[1], g.x))
			case sfnt.SegmentOpCubeTo:
				b.WriteString("C" + pt(s.Args[0], g.x) + " " + pt(s.Args[1], g.x) + " " + pt(s.Args[2], g.x))
			}
		}
		if open {
			b.WriteString("Z")
		}
	}
	return b.String()
}

// Bounds implements Shape.
func (r *GlyphRun) Bounds() (x0, y0, x1, y1 float64) { return r.x0, r.y0, r.x1, r.y1 }

// Draw implements Shape.
func (r *GlyphRun) Draw(z *vector.Rasterizer, a Affine, ss float64) {
	for _, g := range r.glyphs {
		pt := func(p fixed.Point26_6) (float32, float32) {
			x, y := a.Ox+a.Sx*(float64(p.X)/64+g.x), a.Oy+a.Sy*float64(p.Y)/64
			return float32(x * ss), float32(y * ss)
		}
		started := false
		for _, s := range g.segs {
			switch s.Op {
			case sfnt.SegmentOpMoveTo:
				if started {
					z.ClosePath()
				}
				started = true
				x, y := pt(s.Args[0])
				z.MoveTo(x, y)
			case sfnt.SegmentOpLineTo:
				x, y := pt(s.Args[0])
				z.LineTo(x, y)
			case sfnt.SegmentOpQuadTo:
				x1, y1 := pt(s.Args[0])
				x2, y2 := pt(s.Args[1])
				z.QuadTo(x1, y1, x2, y2)
			case sfnt.SegmentOpCubeTo:
				x1, y1 := pt(s.Args[0])
				x2, y2 := pt(s.Args[1])
				x3, y3 := pt(s.Args[2])
				z.CubeTo(x1, y1, x2, y2, x3, y3)
			}
		}
		if started {
			z.ClosePath()
		}
	}
}

// ---- stroked polylines (the SVG pictogram) -------------------------------

type pt2 struct{ x, y float64 }

// Strokes is a set of polylines stroked with a miter join and butt caps.
type Strokes struct {
	polys          [][]pt2 // polygons to fill, all with positive orientation
	x0, y0, x1, y1 float64
}

var (
	reD  = regexp.MustCompile(`<path[^>]*\sd="([^"]+)"`)
	reTr = regexp.MustCompile(`translate\(\s*(-?[\d.]+)[\s,]+(-?[\d.]+)\s*\)`)
	reSW = regexp.MustCompile(`stroke-width="([\d.]+)"`)
	reTk = regexp.MustCompile(`[MHVLZmhvlz]|-?\d+(?:\.\d+)?`)
)

// LoadSVGStrokes reads an SVG made only of absolute M/H/V/L stroked paths,
// optionally under one translate(), with a single stroke-width and miter
// joins (the form of ccdps-mark-v1/v2.svg), and returns the stroked shape.
func LoadSVGStrokes(path string) (*Strokes, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSVGStrokes(string(data), path)
}

// ParseSVGStrokes is LoadSVGStrokes on SVG text; name is used in errors.
func ParseSVGStrokes(src, path string) (*Strokes, error) {
	var tx, ty float64
	if m := reTr.FindStringSubmatch(src); m != nil {
		tx, _ = strconv.ParseFloat(m[1], 64)
		ty, _ = strconv.ParseFloat(m[2], 64)
	}
	m := reSW.FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("%s: no stroke-width", path)
	}
	sw, _ := strconv.ParseFloat(m[1], 64)
	var lines [][]pt2
	for _, d := range reD.FindAllStringSubmatch(src, -1) {
		var cur []pt2
		var cmd string
		var x, y float64
		toks := reTk.FindAllString(d[1], -1)
		num := func(i int) (float64, error) { return strconv.ParseFloat(toks[i], 64) }
		for i := 0; i < len(toks); {
			t := toks[i]
			if len(t) == 1 && (t[0] >= 'A' && t[0] <= 'Z' || t[0] >= 'a' && t[0] <= 'z') {
				cmd = t
				i++
				continue
			}
			switch cmd {
			case "M", "L":
				if i+1 >= len(toks) {
					return nil, fmt.Errorf("%s: truncated path", path)
				}
				x, _ = num(i)
				y, _ = num(i + 1)
				i += 2
				if cmd == "M" {
					if len(cur) > 1 {
						lines = append(lines, cur)
					}
					cur = nil
					cmd = "L"
				}
			case "H":
				x, _ = num(i)
				i++
			case "V":
				y, _ = num(i)
				i++
			default:
				return nil, fmt.Errorf("%s: unsupported path command %q", path, cmd)
			}
			cur = append(cur, pt2{x + tx, y + ty})
		}
		if len(cur) > 1 {
			lines = append(lines, cur)
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%s: no paths", path)
	}
	return buildStrokes(lines, sw), nil
}

func area(p []pt2) float64 {
	var a float64
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].x*p[j].y - p[j].x*p[i].y
	}
	return a / 2
}

func buildStrokes(lines [][]pt2, w float64) *Strokes {
	h := w / 2
	s := &Strokes{x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
	add := func(p []pt2) {
		if area(p) < 0 {
			for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
				p[i], p[j] = p[j], p[i]
			}
		}
		for _, q := range p {
			s.x0, s.x1 = math.Min(s.x0, q.x), math.Max(s.x1, q.x)
			s.y0, s.y1 = math.Min(s.y0, q.y), math.Max(s.y1, q.y)
		}
		s.polys = append(s.polys, p)
	}
	for _, l := range lines {
		for i := 0; i+1 < len(l); i++ {
			p, q := l[i], l[i+1]
			dx, dy := q.x-p.x, q.y-p.y
			n := math.Hypot(dx, dy)
			nx, ny := -dy/n*h, dx/n*h
			add([]pt2{{p.x + nx, p.y + ny}, {q.x + nx, q.y + ny}, {q.x - nx, q.y - ny}, {p.x - nx, p.y - ny}})
		}
		for i := 1; i+1 < len(l); i++ {
			p0, p, p1 := l[i-1], l[i], l[i+1]
			d1x, d1y := p.x-p0.x, p.y-p0.y
			d2x, d2y := p1.x-p.x, p1.y-p.y
			n1, n2 := math.Hypot(d1x, d1y), math.Hypot(d2x, d2y)
			d1x, d1y, d2x, d2y = d1x/n1, d1y/n1, d2x/n2, d2y/n2
			cross := d1x*d2y - d1y*d2x
			if math.Abs(cross) < 1e-9 {
				continue
			}
			// outer side: opposite the turn
			sg := 1.0
			if cross > 0 {
				sg = -1
			}
			a := pt2{sg * -d1y, sg * d1x} // outer unit normal of segment 1
			b := pt2{sg * -d2y, sg * d2x}
			pa := pt2{p.x + a.x*h, p.y + a.y*h}
			pb := pt2{p.x + b.x*h, p.y + b.y*h}
			dot := a.x*b.x + a.y*b.y
			// miter length ratio = 1/sin(theta/2) = sqrt(2/(1+dot)); limit 4
			if math.Sqrt(2/(1+dot)) <= 4 {
				tip := pt2{p.x + h*(a.x+b.x)/(1+dot), p.y + h*(a.y+b.y)/(1+dot)}
				add([]pt2{p, pa, tip, pb})
			} else {
				add([]pt2{p, pa, pb})
			}
		}
	}
	return s
}

// Bounds implements Shape.
func (s *Strokes) Bounds() (x0, y0, x1, y1 float64) { return s.x0, s.y0, s.x1, s.y1 }

// Draw implements Shape.
func (s *Strokes) Draw(z *vector.Rasterizer, a Affine, ss float64) {
	for _, p := range s.polys {
		for i, q := range p {
			x, y := float32((a.Ox+a.Sx*q.x)*ss), float32((a.Oy+a.Sy*q.y)*ss)
			if i == 0 {
				z.MoveTo(x, y)
			} else {
				z.LineTo(x, y)
			}
		}
		z.ClosePath()
	}
}

var _ draw.Image = (*image.Alpha)(nil)
