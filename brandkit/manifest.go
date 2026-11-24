package brandkit

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// MaxFileBytes is the size limit every kit file must stay under.
const MaxFileBytes = 1 << 20

// Entry is one file of the kit.
type Entry struct {
	Name string
	Kind string // svg, png, ico, json
	W, H int    // expected pixel size (png); 0 otherwise
	ICO  []int  // sizes inside an ico
	gen  func(k *Kit) []byte
}

func strokeFor(px int) float64 {
	if px <= SmallMaxPx {
		return SmallStroke
	}
	return StrokeWidth
}

// lockupH is the pixel height of a lockup w pixels wide, lw units wide in the viewBox.
func lockupH(w int, lw float64) int {
	return int(math.Round(float64(w) * TileSize / lw))
}

func svgEntry(name string, sc func(k *Kit) *Scene, v Variant) Entry {
	return Entry{Name: name, Kind: "svg", gen: func(k *Kit) []byte {
		return []byte(sc(k).SVG(v))
	}}
}

func pngEntry(name string, w, h int, radius float64, v Variant, lockup bool) Entry {
	return Entry{Name: name, Kind: "png", W: w, H: h, gen: func(k *Kit) []byte {
		var s *Scene
		if lockup {
			s = k.Lockup(strokeFor(w))
		} else {
			s = k.Mark(radius, strokeFor(w))
		}
		return EncodePNG(s.Raster(v, Canvas{W: w, H: h, Scale: float64(w) / s.W}))
	}}
}

// Manifest lists every file the generator writes. It needs the lockup width,
// so it takes the Kit.
func (k *Kit) Manifest() []Entry {
	l := k.Lockup(StrokeWidth)
	colour, _ := VariantByName("colour")
	inverse, _ := VariantByName("inverse")
	var es []Entry
	for _, v := range Variants {
		v := v
		es = append(es,
			svgEntry("mark-"+v.Name+".svg", func(k *Kit) *Scene { return k.Mark(TileRadius, StrokeWidth) }, v),
			svgEntry("lockup-horizontal-"+v.Name+".svg", func(k *Kit) *Scene { return k.Lockup(StrokeWidth) }, v))
	}
	es = append(es, svgEntry("favicon.svg", func(k *Kit) *Scene { return k.Mark(TileRadius, SmallStroke) }, colour))
	for _, s := range []int{16, 32, 64, 128, 256, 512, 1024} {
		es = append(es, pngEntry(fmt.Sprintf("mark-%d.png", s), s, s, TileRadius, colour, false))
	}
	// opaque, square-cornered: platforms apply their own mask
	es = append(es,
		pngEntry("apple-touch-icon.png", 180, 180, 0, colour, false),
		pngEntry("avatar-512.png", 512, 512, 0, colour, false),
		pngEntry("saml-logo-160.png", 160, 160, TileRadius, colour, false),
		pngEntry("saml-logo-80.png", 80, 80, TileRadius, colour, false))
	for _, w := range []int{600, 1200, 2400} {
		es = append(es,
			pngEntry(fmt.Sprintf("lockup-colour-%d.png", w), w, lockupH(w, l.W), 0, colour, true),
			pngEntry(fmt.Sprintf("lockup-inverse-%d.png", w), w, lockupH(w, l.W), 0, inverse, true))
	}
	es = append(es, Entry{Name: "social-banner-1280x640.png", Kind: "png", W: 1280, H: 640, gen: func(k *Kit) []byte {
		s := k.Lockup(StrokeWidth)
		const lw = 720.0
		sc := lw / s.W
		return EncodePNG(s.Raster(inverse, Canvas{W: 1280, H: 640, Scale: sc, Ox: (1280 - lw) / 2, Oy: (640 - TileSize*sc) / 2, Bg: InkHex}))
	}})
	es = append(es, Entry{Name: "favicon.ico", Kind: "ico", ICO: []int{16, 32, 48}, gen: func(k *Kit) []byte {
		var ps [][]byte
		sizes := []int{16, 32, 48}
		for _, s := range sizes {
			sc := k.Mark(TileRadius, strokeFor(s))
			ps = append(ps, EncodePNG(sc.Raster(colour, Canvas{W: s, H: s, Scale: float64(s) / TileSize})))
		}
		b, err := ICO(ps, sizes)
		if err != nil {
			panic(err) // the sizes above are constants inside ICO's range
		}
		return b
	}})
	es = append(es, Entry{Name: "tokens.json", Kind: "json", gen: func(k *Kit) []byte { return k.tokens() }})
	return es
}

// MinCapPx is the smallest baseline cap height (px) at which the baseline
// text stays legible; the lockup minimum width follows from it.
const MinCapPx = 7.0

// MinLockupWidth is the lockup width in px (rounded up to 10) at which the
// baseline caps are MinCapPx high.
func MinLockupWidth(l *Scene) int {
	return int(math.Ceil(MinCapPx*l.W/l.CapUnits/10)) * 10
}

type tokenDoc struct {
	Colour    map[string]string  `json:"colour"`
	Geometry  map[string]float64 `json:"geometry"`
	Wordmark  map[string]any     `json:"wordmark"`
	MinSizePx map[string]int     `json:"minSizePx"`
	Rules     map[string]string  `json:"rules"`
}

func (k *Kit) tokens() []byte {
	l := k.Lockup(StrokeWidth)
	d := tokenDoc{
		Colour: map[string]string{"ink": InkHex, "paper": PaperHex, "black": BlackHex},
		Geometry: map[string]float64{
			"tile": TileSize, "tileRadius": TileRadius, "strokeWidth": StrokeWidth, "smallStrokeWidth": SmallStroke,
			"clearSpace": ClearSpace, "lockupWidth": math.Round(l.W*100) / 100, "lockupHeight": TileSize,
			"wordGap": WordGap, "wordEm": WordEm, "wordTrackingPer2048Em": WordTrack,
		},
		Wordmark: map[string]any{"text": WordString, "font": "Figtree", "weight": 700, "license": "OFL-1.1", "converted": "outlines, no live text",
			"baseline": BaselineText, "baselineTrackingEm": math.Round(l.BaseTrack*10000) / 10000,
			"baselineCapHeight": math.Round(l.CapUnits*100) / 100},
		MinSizePx: map[string]int{"mark": 16, "markRecommended": 32, "lockupWidth": MinLockupWidth(l)},
		Rules: map[string]string{
			"clearSpace": "6 x stroke width on every side (48 units of the 256-unit tile)",
			"smallCut":   "rasters of 48 px and below use stroke width 10 instead of 8",
		},
	}
	b, _ := json.MarshalIndent(d, "", "  ") // maps of strings and numbers cannot fail
	return append(b, '\n')
}

// Generate writes every entry into dir and returns the names written.
func (k *Kit) Generate(dir string) ([]string, error) {
	// A failure to make dir is not checked here: the first WriteFile below
	// fails for the same reason and reports it.
	_ = os.MkdirAll(dir, 0o755)
	var names []string
	for _, e := range k.Manifest() {
		if err := os.WriteFile(filepath.Join(dir, e.Name), e.gen(k), 0o644); err != nil {
			return nil, err
		}
		names = append(names, e.Name)
	}
	return names, nil
}

// MaxPixelDiff is the largest per-channel difference Check accepts between
// two renderings of the same PNG: float arithmetic differs by CPU (fused
// multiply-add on arm64), so byte equality of PNGs is not portable.
const MaxPixelDiff = 3

// Check verifies the kit in dir: every file exists, is under MaxFileBytes,
// PNGs and ICO entries decode at the expected size, SVGs hold no <text> or
// <image>, tokens parse. When against is non-empty, SVG and JSON must equal
// the files there byte for byte and PNGs must agree to MaxPixelDiff.
func (k *Kit) Check(dir, against string) []string {
	var bad []string
	for _, e := range k.Manifest() {
		p := filepath.Join(dir, e.Name)
		data, err := os.ReadFile(p)
		if err != nil {
			bad = append(bad, e.Name+": "+err.Error())
			continue
		}
		if len(data) >= MaxFileBytes {
			bad = append(bad, fmt.Sprintf("%s: %d bytes, limit %d", e.Name, len(data), MaxFileBytes))
		}
		switch e.Kind {
		case "svg":
			s := string(data)
			for _, banned := range []string{"<text", "<image", "<script", "data:"} {
				if strings.Contains(s, banned) {
					bad = append(bad, e.Name+": contains "+banned)
				}
			}
		case "json":
			var v map[string]any
			if err := json.Unmarshal(data, &v); err != nil {
				bad = append(bad, e.Name+": "+err.Error())
			}
		case "png":
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				bad = append(bad, e.Name+": "+err.Error())
			} else if b := img.Bounds(); b.Dx() != e.W || b.Dy() != e.H {
				bad = append(bad, fmt.Sprintf("%s: %dx%d, want %dx%d", e.Name, b.Dx(), b.Dy(), e.W, e.H))
			}
		case "ico":
			bad = append(bad, checkICO(e, data)...)
		}
		if against != "" {
			bad = append(bad, compare(e, data, filepath.Join(against, e.Name))...)
		}
	}
	return bad
}

func checkICO(e Entry, data []byte) []string {
	if len(data) < 6 || binary.LittleEndian.Uint16(data[2:]) != 1 || int(binary.LittleEndian.Uint16(data[4:])) != len(e.ICO) {
		return []string{e.Name + ": bad ICO header"}
	}
	var bad []string
	for i, s := range e.ICO {
		o := 6 + 16*i
		if len(data) < o+16 {
			return append(bad, e.Name+": truncated directory")
		}
		n, off := int(binary.LittleEndian.Uint32(data[o+8:])), int(binary.LittleEndian.Uint32(data[o+12:]))
		if off < 0 || n < 0 || off+n > len(data) {
			bad = append(bad, fmt.Sprintf("%s: entry %d out of range", e.Name, i))
			continue
		}
		img, err := png.Decode(bytes.NewReader(data[off : off+n]))
		if err != nil {
			bad = append(bad, fmt.Sprintf("%s: entry %d: %v", e.Name, i, err))
		} else if b := img.Bounds(); b.Dx() != s || b.Dy() != s {
			bad = append(bad, fmt.Sprintf("%s: entry %d is %dx%d, want %d", e.Name, i, b.Dx(), b.Dy(), s))
		}
	}
	return bad
}

func compare(e Entry, data []byte, other string) []string {
	o, err := os.ReadFile(other)
	if err != nil {
		return []string{e.Name + ": " + err.Error()}
	}
	switch e.Kind {
	case "svg", "json":
		if !bytes.Equal(data, o) {
			return []string{e.Name + ": differs from " + other}
		}
	case "png":
		return pixelDiff(e.Name, data, o)
	case "ico":
		var bad []string
		for i := range e.ICO {
			p := 6 + 16*i
			if len(data) < p+16 || len(o) < p+16 {
				return []string{e.Name + ": truncated"}
			}
			a := data[binary.LittleEndian.Uint32(data[p+12:]):][:binary.LittleEndian.Uint32(data[p+8:])]
			b := o[binary.LittleEndian.Uint32(o[p+12:]):][:binary.LittleEndian.Uint32(o[p+8:])]
			bad = append(bad, pixelDiff(fmt.Sprintf("%s[%d]", e.Name, i), a, b)...)
		}
		return bad
	}
	return nil
}

func pixelDiff(name string, a, b []byte) []string {
	ia, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		return []string{name + ": " + err.Error()}
	}
	ib, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return []string{name + ": " + err.Error()}
	}
	if ia.Bounds() != ib.Bounds() {
		return []string{name + ": sizes differ"}
	}
	na, nb := toNRGBA(ia), toNRGBA(ib)
	worst := 0
	for i := range na.Pix {
		d := int(na.Pix[i]) - int(nb.Pix[i])
		if d < 0 {
			d = -d
		}
		worst = max(worst, d)
	}
	if worst > MaxPixelDiff {
		return []string{fmt.Sprintf("%s: worst channel difference %d > %d", name, worst, MaxPixelDiff)}
	}
	return nil
}

func toNRGBA(i image.Image) *image.NRGBA {
	b := i.Bounds()
	n := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			n.Set(x, y, i.At(x, y))
		}
	}
	return n
}
