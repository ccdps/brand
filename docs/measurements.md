# Measurements

Everything here was measured on two original captures (a 451 x 138 lockup and
a pictogram PNG) that are NOT in this repository: they are third-party
artwork of unknown rights, so only the numbers derived from them are kept.
The tools that produced the numbers are `cmd/logofit` (fetch, run, trace,
measure); they need the captures as required `--lockup` / `--mark` flags.

## Lockup proportions (original, pixels)

| quantity | value |
|---|---|
| image | 451 x 138, Otsu threshold 138 |
| tile | x 9..135 (127 wide), y 10..126 (117 high) |
| letters | rows 25..83 (59 px); cap height / tile height 0.504 |
| first pen origin right of the tile | 18.6 px; ink-to-tile gap about 21 px, about 5.75 x the pictogram stroke (8/256 x 117 = 3.66 px) |
| baseline text (not reproduced) | x 160..442, rows 105..112, 8 px high |
| fitted em per letter | C 83.24, C 83.35, D 83.52, P 82.03, S 82.56 (mean 82.94) |
| fitted pen origins x | 154.61, 217.43, 280.65, 340.77, 392.53 |
| implied tracking | 0.0278 em (pitch minus Figtree advance, averaged) |
| tile and wordmark colour | #121212 (median of opaque pixels) |

The kit scales these by 256/117 (the kit tile is square, the original tile is
127 x 117): em 181.5, gap 40.7, tracking 57/2048 em, in the 256-unit tile.
The wordmark keeps its measured baseline (row 83.3 of the original, 160.4
units below the tile top). The baseline text is sized so that accent-to-foot is
8 px (17.5 units, bottom edge at row 113) and its tracking (0.0315 em) is
solved so its ink spans the wordmark's ink; the original's own extent
(x 160..442) was measured but is not used for the width.

## Wordmark font

54 candidate free fonts were scored by per-glyph IoU of C, C, D, P, S fitted
(uniform scale and translation) to the isolated letters; 51 were fetched
(Plus Jakarta Sans 900, Manrope 900 and Sora 900 returned HTTP 400).

| font | mean glyph IoU | rank |
|---|---|---|
| Plus Jakarta Sans 800 | 0.920 | 1 |
| **Figtree 700** | **0.910** | 2 |
| Montserrat 800 | 0.771 | 35 |

Figtree 700 (rank 2, 0.010 below rank 1) is used. Both are OFL-licensed and
both are plausible; the two scores are close on a target 59 pixels high, so
the ranking between them is not a strong claim.

Positive control: Montserrat 800 rendered at em 56.00 through the same
pipeline ranks itself 1st with IoU 0.952 and its em is recovered as 56.002.
This is what a perfect candidate scores here, so 0.91 means "close", not
"identical".

## Pictogram redraw

| redraw | IoU | chamfer (px) |
|---|---|---|
| v2 (original SVG redraw) | 0.817 | 0.267 |
| v3 (fitted, stroke width 8, rounded to 0.25) | 0.974 | 0.024 |

Ceiling (v3 against its own binarisation): 0.981. Held-out check (original
upscaled 2x, re-binarised, same affine doubled): v3 0.9655, v2 0.809.
Rounding the fitted coordinates to 0.25 unit costs 0.0006 IoU when the affine
is refitted (0.956 if the old affine is kept).

## Method

IoU is intersection over union of the 0.5-thresholded original against a 4x
supersampled render. Chamfer is the mean nearest-edge distance in pixels. The
rasteriser is `golang.org/x/image/vector`. Otsu thresholds binarise the
captures. The fitting is a pattern search over scale, translation and, for the
pictogram, the geometry variables.
