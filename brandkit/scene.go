// Package brandkit generates the CCDPS brand kit (SVG masters, PNG and ICO
// derivatives, design tokens) from two sources of truth: the v3 pictogram
// SVG (assets/ccdps-mark-v3.svg) and the Figtree 700 outlines (fonts/).
//
// Every number below that is not a design choice was measured on the original
// lockup by `logofit measure` (see docs/measurements.md); the comment on each
// constant says which.
package brandkit

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/ccdps/brand/logofit"
)

// Geometry, in the units of the pictogram viewBox (the tile is 256 x 256).
const (
	TileSize     = 256.0 // viewBox of ccdps-mark-v3.svg
	TileRadius   = 20.0  // rx of the v3 tile
	StrokeWidth  = 8.0   // fitted stroke width of the pictogram (logofit trace)
	SmallStroke  = 10.0  // optical cut for renders of 48 px and below
	SmallMaxPx   = 48    // largest raster size that uses the small cut
	ClearSpace   = 6 * StrokeWidth
	OriginalTile = 117.0 // measured tile height of the original lockup, px
	// Wordmark, measured on the original lockup and scaled by
	// TileSize/OriginalTile (the original tile is 127 x 117 px; the kit tile
	// is square):
	//   mean fitted em 82.940 px          -> 181.5 units
	//   first pen origin 18.612 px right of the tile -> 40.7 units
	//   implied tracking 0.0278 em         -> 57 / 2048 em
	WordEm     = 181.5
	WordGap    = 40.7
	WordTrack  = 57.0
	InkHex     = "#121212" // median colour of the original tile and wordmark pixels
	PaperHex   = "#ffffff"
	BlackHex   = "#000000"
	WordString = "CCDPS"
	// BaselineText is set in Figtree 700 capitals, converted to outlines.
	BaselineText = "CENTRE DE CALCUL ET DE DONNÉES DU PLATEAU-DE-SACLAY"
	// Original vertical placement, measured in original px relative to the
	// 117 px tile (tile top at row 10): mean letter baseline row 83.316; the
	// baseline text's ink (accent to foot) spans rows 105..112, i.e. bottom
	// edge at row 113.
	letterBaselineY = (83.316 - 10) * TileSize / OriginalTile
	baseBottomY     = (113 - 10) * TileSize / OriginalTile
	baseInkHeight   = 8 * TileSize / OriginalTile
)

// Variant is a colour treatment.
type Variant struct {
	Name     string
	Tile     string // tile fill
	Stroke   string // pictogram stroke colour (ignored when Knockout)
	Word     string // wordmark fill
	Knockout bool   // strokes are cut out of the tile (one-colour artwork)
}

// Variants are the four treatments of the kit.
var Variants = []Variant{
	{Name: "colour", Tile: InkHex, Stroke: PaperHex, Word: InkHex},
	{Name: "inverse", Tile: PaperHex, Stroke: InkHex, Word: PaperHex},
	{Name: "white", Tile: PaperHex, Word: PaperHex, Knockout: true},
	{Name: "black", Tile: BlackHex, Word: BlackHex, Knockout: true},
}

// VariantByName returns the named variant.
func VariantByName(n string) (Variant, bool) {
	for _, v := range Variants {
		if v.Name == n {
			return v, true
		}
	}
	return Variant{}, false
}

// Kit holds the sources.
type Kit struct {
	font  *logofit.Font
	paths []string                     // path data of the v3 mark SVG
	cuts  map[float64]*logofit.Strokes // stroked shape per stroke width (StrokeWidth, SmallStroke)
}

var rePath = regexp.MustCompile(`<path d="([^"]+)"/>`)

// New builds a Kit from the mark SVG text and the Figtree 700 TTF bytes.
func New(markSVG string, figtree []byte) (*Kit, error) {
	f, err := logofit.LoadFontRunes("Figtree", 700, figtree, "CDPS"+BaselineText+"E")
	if err != nil {
		return nil, err
	}
	if !strings.Contains(markSVG, `stroke-width="8"`) {
		return nil, fmt.Errorf("mark svg: stroke-width=\"8\" not found")
	}
	var ps []string
	for _, m := range rePath.FindAllStringSubmatch(markSVG, -1) {
		ps = append(ps, m[1])
	}
	// Parse both cuts now, so that nothing after New can fail. A document
	// with no path is refused here too (ParseSVGStrokes: "no paths"), so no
	// separate len(ps) == 0 guard is needed.
	cuts := map[float64]*logofit.Strokes{}
	for _, w := range []float64{StrokeWidth, SmallStroke} {
		s, err := logofit.ParseSVGStrokes(strings.Replace(markSVG, `stroke-width="8"`, `stroke-width="`+num(w)+`"`, 1), "mark")
		if err != nil {
			return nil, err
		}
		cuts[w] = s
	}
	return &Kit{font: f, paths: ps, cuts: cuts}, nil
}

// Scene is the geometry of one artwork.
type Scene struct {
	W, H    float64 // viewBox
	Radius  float64
	StrokeW float64
	Strokes *logofit.Strokes
	paths   []string
	Word    *logofit.GlyphRun // nil for the pictogram alone
	WordAff logofit.Affine    // shape units -> viewBox units
	Base    *logofit.GlyphRun // the baseline text, nil for the pictogram alone
	BaseAff logofit.Affine
	// BaseTrack is the letter-spacing of the baseline text in em, solved so
	// that its ink spans the wordmark's ink; CapUnits is its flat cap height
	// (E) in viewBox units.
	BaseTrack, CapUnits float64
}

// Mark is the pictogram: a 256 x 256 tile with its strokes. strokeW must be
// StrokeWidth or SmallStroke, the two cuts New prepared; anything else is a
// programming error.
func (k *Kit) Mark(radius, strokeW float64) *Scene {
	s, ok := k.cuts[strokeW]
	if !ok {
		panic(fmt.Sprintf("brandkit: no stroke cut of width %v", strokeW))
	}
	return &Scene{W: TileSize, H: TileSize, Radius: radius, StrokeW: strokeW, Strokes: s, paths: k.paths}
}

// Lockup is the pictogram, the wordmark at its measured baseline and the
// baseline text under it.
func (k *Kit) Lockup(strokeW float64) *Scene {
	sc := k.Mark(TileRadius, strokeW)
	run := k.font.RunTracked(WordString, WordTrack)
	x0, _, x1, _ := run.Bounds()
	s := WordEm / logofit.UnitsPerEm
	sc.Word = run
	sc.WordAff = logofit.Affine{Sx: s, Sy: s, Ox: TileSize + WordGap, Oy: letterBaselineY}
	sc.W = sc.WordAff.Ox + s*x1
	inkL, inkR := sc.WordAff.Ox+s*x0, sc.W

	// Baseline text: size so that accent-to-foot is baseInkHeight, then solve
	// the tracking that justifies its ink to the wordmark's ink.
	r0 := k.font.Run(BaselineText)
	bx0, by0, bx1, by1 := r0.Bounds()
	bs := baseInkHeight / (by1 - by0)
	gaps := float64(len([]rune(BaselineText)) - 1)
	track := ((inkR-inkL)/bs - (bx1 - bx0)) / gaps // shape units
	sc.Base = k.font.RunTracked(BaselineText, track)
	bx0, _, _, _ = sc.Base.Bounds()
	sc.BaseAff = logofit.Affine{Sx: bs, Sy: bs, Ox: inkL - bs*bx0, Oy: baseBottomY - bs*by1}
	sc.BaseTrack = track / logofit.UnitsPerEm
	_, ey0, _, ey1 := k.font.Run("E").Bounds()
	sc.CapUnits = bs * (ey1 - ey0)
	return sc
}

// num formats v with at most two decimals and no trailing zeros.
func num(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100+0, 'f', -1, 64)
}

// SVG writes the scene as an SVG document with no <text> element: the
// wordmark is outlines.
func (sc *Scene) SVG(v Variant) string {
	var b strings.Builder
	w, h := num(sc.W), num(sc.H)
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s" role="img" aria-labelledby="t">`+"\n", w, h, w, h)
	b.WriteString(`  <title id="t">CCDPS</title>` + "\n")
	rx := ""
	if sc.Radius > 0 {
		rx = ` rx="` + num(sc.Radius) + `"`
	}
	group := func(stroke string) string {
		var g strings.Builder
		fmt.Fprintf(&g, `<g fill="none" stroke="%s" stroke-width="%s" stroke-linejoin="miter">`, stroke, num(sc.StrokeW))
		for _, p := range sc.paths {
			fmt.Fprintf(&g, `<path d="%s"/>`, p)
		}
		g.WriteString("</g>")
		return g.String()
	}
	if v.Knockout {
		fmt.Fprintf(&b, `  <mask id="k" maskUnits="userSpaceOnUse" x="0" y="0" width="%s" height="%s"><rect width="%s" height="%s" fill="#fff"/>%s</mask>`+"\n",
			num(TileSize), num(TileSize), num(TileSize), num(TileSize), group("#000"))
		fmt.Fprintf(&b, `  <rect width="%s" height="%s"%s fill="%s" mask="url(#k)"/>`+"\n", num(TileSize), num(TileSize), rx, v.Tile)
	} else {
		fmt.Fprintf(&b, `  <rect width="%s" height="%s"%s fill="%s"/>`+"\n", num(TileSize), num(TileSize), rx, v.Tile)
		b.WriteString("  " + group(v.Stroke) + "\n")
	}
	if sc.Word != nil {
		fmt.Fprintf(&b, `  <path fill="%s" d="%s"/>`+"\n", v.Word, sc.Word.PathD(sc.WordAff))
	}
	if sc.Base != nil {
		fmt.Fprintf(&b, `  <path fill="%s" d="%s"/>`+"\n", v.Word, sc.Base.PathD(sc.BaseAff))
	}
	b.WriteString("</svg>\n")
	return b.String()
}
