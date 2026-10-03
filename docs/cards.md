# CARD SHOW: the card shinethrough lightshow

`cartridge/pokecard.go` is the hero cart: it backlights a physical trading card
laid on the LCD. Light diffuses through the card stock, so the show does **not**
reproduce the art — it drives a coarse, soft **glow mask** derived from the art
box, tinted by a small palette and animated by independent effect layers. This
page is the full picture. The [README](../README.md) has the one-line summary and
the gallery.

## Effects

Each effect is a separate on/off layer, and any combination composes (the strobe
from breathing + holo is fair game):

- **BREATHE** — a ~3 s brightness envelope, per-pixel and through the backlight
  PWM;
- **HOLO** — a diagonal brightness sweep across the art;
- **SPARKLE** — drifting white glints;
- **flash** — momentary, on the A press edge (not a toggle).

## Colour look

The finished frame is mapped through one colour mode (`RGB`, `BGR`, `SWAP16`,
`BGR+SWAP16`, `HUE`, `OIL`, `FLUX`, `FOIL`, `TINT`, `VIVID`). The mapping is the
*final* post-process, so it also covers the effects: the luminance-driven modes
(`FOIL`, `TINT`, `VIVID`) therefore read the effects' brightness. That is
deliberate — it keeps sparkles and holo tinted — but it does mean effects and
colour are not strictly orthogonal.

The mode is cycled on the joystick **Click** (unused elsewhere) and named in a
short `CMAP …` toast, so the correct one can be confirmed on hardware without a
rebuild. The cycle exists partly as insurance against panel colour-order
regressions; see [display.md](display.md).

- `RGB` — identity, the correct mapping.
- `BGR` — the pre-fix R/B swap.
- `SWAP16` — the pixel's two bytes swapped (the classic endianness look).
- `BGR+SWAP16` — both.
- `HUE` — an animated, luminance-preserving hue rotation: each frame is
  RGB → YCbCr, the chroma is turned by a slowly advancing angle, then converted
  back. Horsea's blue goes magenta at +120°, Electrode's red goes green.
- `OIL` — the same bit-rotation family as `SWAP16`, but rotated 6 bits instead
  of a full byte, so more high-order edge bits stay in the high-order channels:
  still an iridescent oil-slick, but the silhouette survives.
- `FLUX` — the PLASMA cart's animated field rotates each pixel's *own* hue,
  while saturation and luminance stay the art's.
- `FOIL` — the plasma tint screen-blended over the frame, weighted by luminance:
  a holographic sheen that leaves dark lines dark.
- `TINT` — the art multiplied by a plasma colour: an animated coloured gel.
- `VIVID` — a static saturation/vibrancy boost, no recolour.

`FLUX`, `FOIL`, and `TINT` sample the same `buildPlasmaField()` the PLASMA cart
uses and advance it by two per frame, so they move at the plasma's own rate.
`HUE` and the four plasma modes share one trick: they move **chroma only**,
leaving luminance — which carries the artwork's outlines — untouched, so the card
floods with moving colour without dissolving into noise (which is what `SWAP16`
does).

## Controls

- **Left/Right** cycle the loaded card library.
- **Select** toggles the alignment overlay (joystick nudges the mask, A/B rotate
  it).
- **A** fires the attack flash.
- **Start** opens the effects menu.
- **Click** cycles the colour mapping.
- **B** toggles breathing.
- **Start+Select** still exits the cart.

## The effects menu

Press **Start** to edit every setting with the stick: `Up/Down` move the row,
`Left/Right` change the value (colour and card wrap, booleans toggle), `B` (or
Start) closes. The show keeps rendering behind a dimmed panel so a change
previews immediately, and the menu is drawn *after* the colour map so its text is
legible on any look mode. Rows:

```
COLOUR    FLUX
CARD      HORSEA
BREATHE   ON
HOLO      ON
SPARKLE   ON
```

Turning breathing off holds the per-pixel envelope at full and the backlight at
full (the boost converter only lights predictably at full duty — see
[display.md](display.md)).

## Assets: how a card becomes a glow mask

Because diffusion washes out fine detail, each asset is tiny (a 4-bit mask
stretched across the panel and bilinearly upscaled). Assets are **generated at
build time and gitignored** — no card imagery and no derived mask is committed.
`make build`/`size`/`test`/`sim` run `tools/make_card.py`:

- if the shared library's `manifest.json` exists, it bakes one `CardAsset` per
  entry (see [the shared card library](toolchain.md#one-card-library-across-worktrees));
- otherwise it emits a single **original synthetic sample**, so a fresh clone
  and CI still compile (CI installs Pillow for this).

Masks separate the card's **subject from its background** automatically: by
default the generator runs a U2-Net saliency matte (`rembg`, CPU) over the art
box and gates the stretched luminance by that silhouette, so the creature glows
out of a near-black ambient wash instead of rendering a flat luminance photo.
Fallbacks, no per-card config needed:

- `rembg`/`onnxruntime` not installed (or the model download fails) → plain
  stretched luminance, with a `make_card:` warning naming the affected cards;
- `"subject": "luma" | "sat" | "luma*sat"` in a manifest entry → the manual
  derivations (`sat` = color-saturation channel, percentile-stretched);
- `"invert": true` → inverted luminance (line-art look), bypassing the matte;
- `"mask": "file.png"` → your own painted glow/silhouette, always winning.

The bake also derives an `ambient` tint per card (a dim shade of the signature
color; overridable via `"ambient": [r, g, b]`). The render keeps that wash moving
slowly under the subject while breathing it with the global envelope, so
foreground and background stay separated even as everything diffuses.

## Manifest format

Source images live under the gitignored shared library. A manifest entry:

```json
{"file": "charizard.webp", "name": "CHARIZARD", "set": "CLASSIC",
 "types": ["Fire"], "art_box": [22, 96, 378, 295],
 "lcd_window": "fit", "glow_color": [255, 120, 30]}
```

Key flags (CLI or manifest fields):

- `art_box: L T R B` — the illustration window in source pixels.
- `lcd_window: l t r b`, or `"fit"` — the part of the art box that sits over the
  panel. The panel (35×28 mm) is smaller than the art box, so cropping to it
  makes the glow line up 1:1 with the print instead of showing a scaled-down
  picture of the whole art. `"fit"` centres a panel-sized window automatically;
  it is the one measurement the mechanical build will refine.
- `subject` — `auto` (default: U2-Net matte, see above), `luma`, `sat`, or
  `luma*sat`; `--subject` is the single-card CLI equivalent.
- `glow_color: R G B` — override the signature color (a busy card background can
  otherwise dominate the auto-picked color).
- `ambient: R G B` — override the background wash tint (default: a dim shade of
  the signature color).
- `invert` — glow where the art is *dark* (a line-art/negative look).
- `mask: file` — paint your own glow/silhouette (alpha or grayscale) and use it
  instead of luminance; the way to get a true silhouette when the auto matte
  picks the wrong region.
- `gamma`/`floor` — contrast shape of the derived glow.

Single-card CLI:

```sh
python3 tools/make_card.py assets/cards/your-card.webp --art-box L T R B \
    --lcd-fit --glow-color 255 120 30 --name CHARIZARD --set CLASSIC \
    --types Fire -o cartridge/cards_data.go --preview-dir sim-out/card-previews
```

## Art mode

Art-mode assets carry `Art`/`ArtW`/`ArtH` instead of a mask: a full-vibrance
RGB565 image of the card's art box, packed little-endian as a Go string so the
compiler keeps it in flash `rodata` (no RAM copy at boot). The runtime renders it
directly and plays the animation effects (breathing, holo band, flash, sparkles)
on top. Mask and Palette are then unused. This is the current baked format; the
glow-mask path remains for the synthetic sample and for manual masks.

## Calibration

Calibration is per card and per session (RAM only). The Select overlay draws the
card name, a border, a crosshair, and a rotation tick; the joystick nudges the
mask and **A**/**B** step the rotation until the glow sits under the printed art.
`Select`'s overlay also shows the current name and `n/total` index.
