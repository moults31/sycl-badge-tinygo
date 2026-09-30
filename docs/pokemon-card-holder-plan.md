# SYCL Badge V2 — Pokémon Card Holder (Design Plan)

Status: planning (pre-CAD)
Target hardware: SYCL Badge V2 (revision 2), RP2354B board
Source of truth for hardware: reference repo `sycl-badge` (Zig + KiCad + 3D)

## Goal

A simple clip / holder that attaches to the badge and holds a **penny-sleeved
Pokémon card flat against the LCD**, so the screen shines through the card's
illustration ("art") window.

## Decisions locked in

| Decision | Choice |
| --- | --- |
| CAD tool | **Fusion 360** (parametric) |
| Attachment | **4× M3 heat-set inserts** in the printed part; M3 bolts pass through the board's four mounting holes from the back |
| Insert kit | Preciva 716-pc heat-set insert set (M2–M6) — M3 |
| Card orientation | **Portrait** (card's 88.9 mm axis vertical) |
| Insertion | **Vertical slide-in from the top** |
| Bezel style | **Card-outline bezel, top/bottom rails only** (no side walls) — split into two bracketed bars |
| Nav cap | **Printed dpad cap removed** for a flush fit |
| Card plane | **Flush on the LCD glass** (no shroud) |

## Verified mechanical specs

### Board (source: `sycl-badge/kicad/v2/SYCL Badge 2024.kicad_pcb`, `production/.../Edge_Cuts.gm1`)

Coordinates below are KiCad PCB coordinates in mm (**y increases downward**).

* Outline: **110.008 × 64.006 mm**
  * x: 96.406 → 206.414, y: 72.100 → 136.106
* Corner radius: **R3.0 mm** (all four corners)
* Thickness: **1.6 mm**, FR4, 4-layer
* Top-edge notch: **6.4 × 1.5 mm** at x 151.70 → 158.10 (clearance for the power slider)
* Mounting holes: **4× Ø3.0 mm**, plated, 6 mm copper pad
  * H2 (100.455, 76.170), H3 (202.335, 76.150), H4 (100.445, 132.080), H5 (202.305, 132.050)
  * Grid: **101.88 × 55.91 mm**, ~4.05 mm inset from each edge

### Front controls (all on F.Cu — the user-facing side)

Bodies and courtyard extents, in board coordinates:

| Ref | Part | X range | Y range | Height above board |
| --- | --- | --- | --- | --- |
| A1 | SMD tact 6×6 | 192.28–198.78 | 97.92–107.92 | ~3.45 mm (model) |
| B1 | SMD tact 6×6 | 183.75–190.25 | 101.89–111.89 | ~3.45 mm |
| START1 | SMD tact 6×6 | 187.35–197.35 | 72.30–78.80 | ~3.45 mm |
| SELECT1 | SMD tact 6×6 | 108.10–118.10 | 72.20–78.70 | ~3.45 mm |
| U3 | 5-way nav 2425755-1 | ~103.1–122.6 | ~95.6–115.0 | **measure** (~8 mm per model, incl. legs?) |
| J5 | Hirose FH12-15S-0.5SH FPC (LCD) | 143.35–151.25 | 97.39–111.01 | — |

Notes:
* The buttons are **SMD tact switches ~3.45 mm tall** (the footprint name
  `..._H9.5mm` is a KiCad library name, not the installed part's height).
* The printed dpad cap (`sycl-badge/attachments/dpad_v1.3mf`, 22.4 × 22.4 × 7 mm)
  is **excluded** from this design.
* LEDs (5× SK6812, y≈132), JST connectors, battery holder, MCU, USB-C and the
  power switch are all on the **back (B.Cu)**.

### LCD module

Source: `sycl-badge/kicad/v2/packages3D/JD-T18003-T01-Body.stp`,
`packages3D/lcd.FCStd`, `docs/JD-T1800.pdf`.

* 1.8", 160×128, ST7735S-class panel (TinyGo repo calls it `DT018BTFT-SHB`).
* Module outline (STEP bbox): **34.0 × 45.82 mm**, thickness ≈ **2.64 mm**;
  FPC tail extends ~3.8 mm past one long edge.
* Active area: **28.03 × 35.04 mm** (128 × 160 @ 0.219 mm), driven landscape.
* Front-mounted; FPC connector J5 at (146.35, 104.20), rot −90°.
* Datasheet mechanical drawing is embedded as raster images in `JD-T1800.pdf`.

### Card and sleeve

* Bare standard Pokémon card: **63.5 × 88.9 mm**, corner R≈3.18 mm, ~0.30 mm thick.
* Penny sleeve (Ultra Pro / BCW standard): **66.7 × 92.1 mm** (2⅝ × 3⅝ in),
  film 45 µm, adds ~0.09 mm thickness.
* Art window size and offset from the card edges: **to be measured** (no public
  standard).

## Geometry analysis

### Width (the binding constraint)

Sleeved card width **66.7 mm**. Clear channels between the tall front controls:

* nav right edge → B left edge: **~61.4 mm**
* SELECT/START right edge → B/A left edge (courtyard): **~65.7 mm**
* using true 6 mm button bodies: **~67.9 mm**

Consequence: a sleeved card **cannot** be placed so it clears *every* front
control. It must overhang one control's footprint. Because the card plane
(~2.6–3.0 mm) and the SMD buttons (~3.45 mm) are within ~1 mm, small plan-view
overlaps near a button are *near-level* and tolerable; a large overlap is not.

Working assumption: shift the card ~1 mm so its right edge just clears B and its
left edge floats over only the outer edge of the (now cap-less, low) nav. Verify
with calipers; if the sleeve binds, **trim ~1 mm off one long edge of the
sleeve**.

### Vertical / overhang

A sleeved card centered on the LCD center (y ≈ 104) spans y ≈ 58 → 150, i.e.
**~14 mm above and below the board**. The frame therefore extends beyond the
board top and bottom; side stops can live there (above/below the board) with no
control conflicts.

### Placement

The card will be positioned by aligning the **art window** to the **LCD active
area**, not by centering the card. The art window is much larger than the active
area, so there is roughly ±10 mm of placement freedom in both axes.

## Proposed design

* **Two independent bracket bars** (top and bottom), each fastened to two of the
  board's M3 holes via heat-set inserts.
  * Bottom bar: closed card stop.
  * Top bar: open; card drops in from the top.
* **No side walls.** Lateral + vertical registration via short tabs at the rail
  ends, located above/below the board where nothing can collide.
* Card rests directly on the LCD glass; the art window is positioned over the
  active area.
* Recommended tolerances (to refine on a fit-check coupon): slide clearance
  ~0.3–0.4 mm/side, insert pilot sized for the kit's M3 inserts.

## Fabrication / material notes

* Board mounting holes are exactly Ø3.0 mm — line-to-line for an M3 shank. Try
  passing an M3 bolt first; if it binds, either ream to ~3.2 mm (1/8" bit in a
  pin vise, hand-turned) or step down to M2.5. Reaming the plated hole is
  harmless (6 mm pad).
* Print a **fit-check coupon** (the two posts + card channel + a card gauge)
  before the full part.

## Measurement checklist (calipers)

1. Stock nav (U3) height above the board — decides how much the card can pass
   over it.
2. Actual **sleeved-card** width × height (and bare-card width as fallback).
3. **Art window** width × height and offsets from the card's top and side edges.
4. **LCD active-area center** relative to the four mounting holes.
5. Board front face → LCD glass top surface (glass height).

## Build plan

1. **Reference geometry** — export board `Edge_Cuts` to DXF; import the LCD
   `JD-T18003` STEP; model the card + art window from measurements.
2. **Placement** — set the card so the art window covers the active area, with
   the ~1 mm right-shift that clears the tall buttons.
3. **Model** — two bracket bars: insert posts at the four holes, rails capturing
   the card top/bottom, end stops.
4. **Verify** — print the fit-check coupon; check slide fit and control
   clearance; iterate.
5. **Full part** — print, install M3 heat-set inserts, assemble with the board.

## Open items

* Caliper measurements (checklist above).
* Confirm the nav-overlap outcome is acceptable (cap-less nav edge only).
* Decide vertical position once the art-window offset is measured.
