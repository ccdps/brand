package brandkit

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"strconv"

	"golang.org/x/image/vector"

	"github.com/ccdps/brand/logofit"
)

// rrect is a rounded rectangle at the origin, as a logofit.Shape.
type rrect struct{ w, h, r float64 }

func (q rrect) Bounds() (float64, float64, float64, float64) { return 0, 0, q.w, q.h }

func (q rrect) Draw(z *vector.Rasterizer, a logofit.Affine, ss float64) {
	p := func(x, y float64) (float32, float32) {
		return float32((a.Ox + a.Sx*x) * ss), float32((a.Oy + a.Sy*y) * ss)
	}
	const k = 0.5522847498307936 // cubic approximation of a quarter circle
	r, c := q.r, q.r*k
	mv := func(x, y float64) { px, py := p(x, y); z.MoveTo(px, py) }
	ln := func(x, y float64) { px, py := p(x, y); z.LineTo(px, py) }
	cu := func(x1, y1, x2, y2, x3, y3 float64) {
		a1, b1 := p(x1, y1)
		a2, b2 := p(x2, y2)
		a3, b3 := p(x3, y3)
		z.CubeTo(a1, b1, a2, b2, a3, b3)
	}
	mv(r, 0)
	ln(q.w-r, 0)
	cu(q.w-r+c, 0, q.w, r-c, q.w, r)
	ln(q.w, q.h-r)
	cu(q.w, q.h-r+c, q.w-r+c, q.h, q.w-r, q.h)
	ln(r, q.h)
	cu(r-c, q.h, 0, q.h-r+c, 0, q.h-r)
	ln(0, r)
	cu(0, r-c, r-c, 0, r, 0)
	z.ClosePath()
}

func hex(s string) (r, g, b float64) {
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil || len(s) != 7 {
		panic("brandkit: bad colour " + s)
	}
	return float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255
}

// Canvas places a scene on a cw x ch bitmap: viewBox unit u lands at
// (ox + scale*u, oy + scale*u). Bg, when non-empty, is an opaque background.
type Canvas struct {
	W, H      int
	Scale     float64
	Ox, Oy    float64
	Bg        string
	Supersamp int // 0 = chosen from the size
}

// Raster draws the scene in the variant.
func (sc *Scene) Raster(v Variant, c Canvas) *image.NRGBA {
	ss := c.Supersamp
	if ss == 0 {
		ss = 4
		if c.W*c.H < 600*600 {
			ss = 8
		}
	}
	a := logofit.Affine{Sx: c.Scale, Sy: c.Scale, Ox: c.Ox, Oy: c.Oy}
	tile := logofit.Render(rrect{TileSize, TileSize, sc.Radius}, a, c.W, c.H, ss)
	stroke := logofit.Render(sc.Strokes, a, c.W, c.H, ss)
	var word logofit.Cov
	if sc.Word != nil {
		wa := logofit.Affine{Sx: c.Scale * sc.WordAff.Sx, Sy: c.Scale * sc.WordAff.Sy,
			Ox: c.Ox + c.Scale*sc.WordAff.Ox, Oy: c.Oy + c.Scale*sc.WordAff.Oy}
		word = logofit.Render(sc.Word, wa, c.W, c.H, ss)
	}
	var base logofit.Cov
	if sc.Base != nil {
		ba := logofit.Affine{Sx: c.Scale * sc.BaseAff.Sx, Sy: c.Scale * sc.BaseAff.Sy,
			Ox: c.Ox + c.Scale*sc.BaseAff.Ox, Oy: c.Oy + c.Scale*sc.BaseAff.Oy}
		base = logofit.Render(sc.Base, ba, c.W, c.H, ss)
	}
	tr, tg, tb := hex(v.Tile)
	var sr, sg, sb float64
	if !v.Knockout {
		sr, sg, sb = hex(v.Stroke)
	}
	wr, wg, wb := hex(v.Word)
	var br, bg, bb, ba float64
	if c.Bg != "" {
		br, bg, bb = hex(c.Bg)
		ba = 1
	}
	img := image.NewNRGBA(image.Rect(0, 0, c.W, c.H))
	for i := 0; i < c.W*c.H; i++ {
		// premultiplied accumulation, painter's order: background, tile,
		// strokes, wordmark
		pr, pg, pb, pa := br*ba, bg*ba, bb*ba, ba
		over := func(r, g, b, cov float64) {
			pr, pg, pb, pa = r*cov+pr*(1-cov), g*cov+pg*(1-cov), b*cov+pb*(1-cov), cov+pa*(1-cov)
		}
		t := tile.V[i]
		if v.Knockout {
			t *= 1 - stroke.V[i]
		}
		over(tr, tg, tb, t)
		if !v.Knockout {
			over(sr, sg, sb, stroke.V[i]*tile.V[i])
		}
		if sc.Word != nil {
			over(wr, wg, wb, word.V[i])
		}
		if sc.Base != nil {
			over(wr, wg, wb, base.V[i])
		}
		if pa > 0 {
			o := i * 4
			img.Pix[o] = q8(pr / pa)
			img.Pix[o+1] = q8(pg / pa)
			img.Pix[o+2] = q8(pb / pa)
			img.Pix[o+3] = q8(pa)
		}
	}
	return img
}

func q8(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v*255)))) }

// EncodePNG encodes deterministically at the best compression.
func EncodePNG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	e := png.Encoder{CompressionLevel: png.BestCompression}
	if err := e.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ICO packs PNG images (each a square of the given size) into one .ico.
func ICO(pngs [][]byte, sizes []int) ([]byte, error) {
	if len(pngs) != len(sizes) || len(pngs) == 0 {
		return nil, fmt.Errorf("ico: %d images, %d sizes", len(pngs), len(sizes))
	}
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	w(uint16(0))
	w(uint16(1))
	w(uint16(len(pngs)))
	off := 6 + 16*len(pngs)
	for i, p := range pngs {
		s := sizes[i]
		if s < 1 || s > 256 {
			return nil, fmt.Errorf("ico: size %d out of range", s)
		}
		d := byte(s)
		if s == 256 {
			d = 0
		}
		b.Write([]byte{d, d, 0, 0})
		w(uint16(1))  // planes
		w(uint16(32)) // bits per pixel
		w(uint32(len(p)))
		w(uint32(off))
		off += len(p)
	}
	for _, p := range pngs {
		b.Write(p)
	}
	return b.Bytes(), nil
}
