# SYCL Badge V2 — Pokémon Card Holder (Design Plan)

Status: CAD in progress — reference geometry built and verified in `cad/`
Target hardware: SYCL Badge V2 (revision 2), RP2354B board
Source of truth for hardware: reference repo `sycl-badge` (Zig + KiCad + 3D)

## Goal

A holder that attaches to the badge and holds a **penny-sleeved Pokémon card
flat against the LCD**, so the screen shines through the card's illustration
("art") window. The card is positioned by eye over the lit panel and locked with
a **friction clamp**, rather than being forced into a fixed orientation by the
mount.

## Decisions locked in

| Decision | Choice |
| --- | --- |
| CAD tool | **Fusion 360** (parametric) |
| Attachment | **4× M3 heat-set inserts** in the printed part; M3 bolts pass through the board's four mounting holes from the back |
| Insert kit | Preciva 716-pc heat-set insert kit — **M3, OD 5 mm, lengths 6/8 mm** |
| Card orientation | **Portrait** (card's long axis vertical) |
| Placement | Card placed **art-window-over-active-area**; bottom-heavy overhang accepted |
| Retention | **Screw clamp with friction lock**: a loose nominal cradle sets rough placement, printed thumb-screw clamps take up the slack and lock it (fine trim, a few mm / ~2°) |
| Clamp contact | **Over the card face (through the stack)**, confined to the outer margin **outside the art box** |
| Clamp cartridges | **Swappable** contact elements — point pad vs. short rail — evaluated on a coupon |
| Clamp material | **▸ M3 heat-set insert + stock M3 socket-cap bolt** in the arm (changed from a printed PETG thumb thread, 2026-09-30); PETG for the compliant face; PLA/PETG for the rigid bars — **no TPU on hand** |
| Over-torque control | Printed **PETG flexure face** + a hard **travel stop** (~stack − 0.2 mm) |
| Card plane | **Flush on the LCD glass** (no shroud) |

## Verified mechanical specs

### Board (source: `sycl-badge/kicad/v2/SYCL Badge 2024.kicad_pcb`)

Coordinates are KiCad PCB coordinates in mm (**y increases downward**).

* Outline: **110.007 × 64.015 mm** (x 96.406 → 206.414, y 72.091 → 136.106),
  centre (151.410, 104.099). The earlier "110.008 × 64.006, centre 151.41,
  104.10" was rounded.
* Corner radius **R3.0 nominal**. The drawn arcs are hand-authored
  start/mid/end points and are **not** a clean R3.0: top-left and top-right are
  R3.000, bottom-left **R3.022**, bottom-right **R3.122**. The model keeps the
  drawn arcs rather than idealising them.
* Thickness **1.6 mm** FR4, 4-layer
* Top-edge notch **6.4 × 1.5 mm** at x 151.70 → 158.10 (power slider)
* Mounting holes: **4× Ø3.0 mm**, plated, 6 mm pad
  * H2 (100.455, 76.170), H3 (202.335, 76.150),
    H4 (100.445, 132.080), H5 (202.305, 132.050)
  * **Not a perfect rectangle**: spans are top x 101.880, bottom x 101.860,
    left y 55.910, right y 55.900. The "101.88 × 55.91 grid" is the
    H2/H3 × H2/H4 pair only, and the ~4.05 mm inset is nominal.
  * These holes are the **mating datum** for the holder. Use the exact per-hole
    XY from `cad/reference/board.json`, never the rounded grid.

### Front-side parts (F.Cu — the user-facing side)

The board carries **17 F.Cu footprints**. The six that were tabulated when this
plan was written are below; rows marked **▸** were added in the 2026-09-30
review and come from the board file, not from assumption. The full derived list
(pad counts, layer sets, rotation) is `cad/reference/board.json` →
`front_footprints`, produced by `cad/reference/kicad_extract.py`.

| Ref | Part | X range | Y range | Height above board |
| --- | --- | --- | --- | --- |
| A1 | SMD tact 6×6 | 192.28–198.78 | 97.92–107.92 | 3.45 body / **5.0 actuator** |
| B1 | SMD tact 6×6 | 183.75–190.25 | 101.89–111.89 | 3.45 body / **5.0 actuator** |
| START1 | SMD tact 6×6 | 187.35–197.35 | 72.30–78.80 | 3.45 body / **5.0 actuator** |
| SELECT1 | SMD tact 6×6 | 108.10–118.10 | 72.20–78.70 | 3.45 body / **5.0 actuator** |
| U3 | 5-way nav 2425755-1 | ~103.1–122.6 | ~95.6–115.0 | **8.00** |
| J5 | Hirose FH12-15S-0.5SH FPC (LCD) | 143.35–151.25 | 97.39–111.01 | — |
| **▸** D1, D2, D4, D5, D6 | SK6812MINI addressable LED | centres x 135.78, 143.78, 151.72, 159.70, 167.65 (pitch ≈7.9) | 130.2–134.2 | **0.75** (`neopixel.stp`); ~1.6 typical |
| **▸** Q1 | ALS-PT19 ambient light sensor, 0603 | 166.77–169.73 | 73.22–74.69 | ≈0.8 |
| **▸** H1 | microzig logo — flat, padless (copper + silk) | 105.45–118.34 | 117.07–129.96 | ≈0 |

* **▸ The five LEDs sit in a row at y ≈ 132.2 — the same line as the H4/H5
  bolts (y ≈ 132.0).** The bottom bracket's field is therefore *not* clear, and
  it must not sit flat on the board there. It doesn't: the bracket's back plate
  is carried on the insert bosses, putting its underside at **z = 3.6 mm**,
  which clears the tallest LED by ≥2.0 mm. **No relief pockets are needed.**
* The card plane sits at **6.00 mm**, so it clears every tact switch (5.0 mm
  actuator, 3.45 mm body) and every part above. **Only the nav (8.00 mm)
  protrudes — by 2.0 mm.**
* No LCD body footprint exists on the PCB — only the FPC connector J5. The
  module is held mechanically, so its on-board position is a build property.

### LCD module

Source: `sycl-badge/kicad/v2/packages3D/JD-T18003-T01-Body.stp`, `docs/JD-T1800.pdf`.

* 1.8", 160×128, ST7735S-class panel; module outline **34.0 × 45.82 mm**;
  bare module thickness ≈ **2.64 mm**.
* Driven landscape: the lit active rectangle is **35.04 wide (x) × 28.03 tall
  (y)** on the board (confirmed by measurement, below).
* **Glass-top height above the board front face = 6.00 mm** (confirmed). This is
  well above the bare 2.64 mm module — the panel stands off the board in the
  real assembly. Treat 6.00 as the **card rest plane**.

### Card and sleeve

| Item | Nominal | Measured |
| --- | --- | --- |
| Bare card W × H | 63.5 × 88.9 | **63.0 × 87.5** |
| Sleeved card W × H | 66.7 × 92.1 | **66.0 × 94.0** |
| Stack thickness (card + sleeve) | ~0.39 | **~0.40** |
| Art window W × H | no public standard | **≈ 51 × 33** (derived) |
| Art-window insets L / R / T / B | — | **≈ 6 / 6 / 13 / 43** (derived) |
| Art-window centre, from card top | — | **≈ 29.5 mm** (≈15 mm above card centre) |

## Measured values (calipers, 2026-09-30)

| # | Measurement | Expected | Actual |
| --- | --- | --- | --- |
| 1 | Sleeved card width | ~66.7 | 66.0 |
| 2 | Sleeved card height | ~92.1 | 94.0 |
| 3 | Bare card width | 63.5 | 63.0 |
| 4 | Bare card height | 88.9 | 87.5 |
| 5 | Sleeved stack thickness | ~0.39 | ~0.40 |
| 6 | Art-window width | derive | ≈ 51 |
| 7 | Art-window height | derive | ≈ 33 |
| 8 | Art-window insets T/B/L/R | ~2–8 | ≈ 13 / 43 / 6 / 6 |
| 9 | Art-window corner radii | — | not needed (clamps stay in the margins) |
| 10 | LCD active width | 28.03 | **34.5** (landscape x) |
| 11 | LCD active height | 35.04 | **27.5** (landscape y) |
| 12 | Active-area edges from board edges L/R/T/B | derived | **40.0 / 35.5 / 14.0 / 22.5** |
| 13 | Glass height (front face → glass) | ~2.64 | **6.00 (confirmed)** |
| 14 | Board thickness | 1.60 | 1.60 |
| 15 | Mounting-hole bore | 3.00 | 3.00 |
| 16 | Nav (U3) height above board | measure | 8.00 |
| 17 | Tact height | ~3.45 | **3.45 body / 5.0 actuator** |
| 18 | Insert barrel OD | measure | **5.0** (kit label) |
| 19 | Insert flange OD / height | measure | 5.0 / small (kit label) |
| 20 | Insert barrel length | measure | **6 or 8** (kit label) |
| 21 | Bolt shank length | measure | **8 / 12 / 16 / 20** (kit label) |
| 22 | Bolt head Ø / height | measure | ~5.5 / ~3.0 nominal M3 socket cap (verify) |
| 23 | Bolt thread pitch | 0.5 | 0.5 (M3 coarse) |

### Derived active-area position (#12)

From the edge offsets: active rectangle **x 136.41 → 170.91** (w 34.5),
**y 86.10 → 113.61** (h 27.5), i.e. **centre (153.66, 99.85)** — offset
**+2.25 mm right and 4.25 mm above** the board centre. (Offsets are ±~1 mm;
the measured 34.5 × 27.5 vs the 35.04 × 28.03 datasheet is consistent with
reading to the visible edge.)

### Deriving the art window

No public standard exists, so it was derived from the in-repo card images
(`assets/cards/*.webp`, gitignored) by scaling image width to the 63.5 mm card
width and locating the illustration frame via row/column texture profiles plus
visual overlay. The two cleanest cards:

| Card | W × H (mm) | Insets L / R / T / B (mm) | Art-centre from top (mm) |
| --- | --- | --- | --- |
| charizard-classic | 51.4 × 31.4 | 5.2 / 6.8 / 14.6 / 43.0 | 30.2 |
| magikarp-base | 50.3 × 35.1 | 6.5 / 6.7 / 11.4 / 42.2 | 29.0 |

**Representative: W ≈ 51, H ≈ 33, insets L/R ≈ 6, centre ≈ 29.5 mm from the
card top** (±1–2 mm until a flatbed scan refines it).

**Placement budget:** panel 35.04 × 28.03 against a ~51 × 33 window leaves
**±8 mm horizontal but only ±2.5 mm vertical** trim. Vertical is the tight axis.

## Inserts and fasteners (Preciva 716-pc kit)

* **M3 heat-set inserts**: OD **5 mm**; lengths **6 mm** (30 pcs) and **8 mm** (25 pcs).
* **M3 socket-head cap screws**: lengths **8 / 12 / 16 / 20 mm**.
* **M3 nuts ×55, M3 spacers ×55** (kit also has M2/M4/M5/M6).

* **Insert pilot** in the printed boss: size to the 5 mm OD insert; expect
  ~4.0–4.5 mm in PETG — dial on the coupon.
* **▸ Use the 6 mm inserts, not the 8 mm.** Available material is exactly the
  6.00 mm from the board face (z = 0) to the card rest plane (z = 6.00); an 8 mm
  insert would break through into the board. The insert is installed from the
  **underside** (the board-facing face), because the top face is the card plane.
* **▸ Bolt length, and the 0.4 mm overshoot.** An 8 mm M3 bolt passes 1.6 mm of
  board and then enters 6.4 mm into a 6.0 mm insert — the tip would emerge at the
  card plane. **Fit a ≈0.5 mm spacer under each head** (the kit has M3 spacers),
  which pulls the tip back to ≈5.9 mm.
* **▸ Verify the head clears** the back-side AAA holder / USB-C / JST **with the
  spacer fitted**, at the chosen length.

## Geometry analysis

### Heights

Glass = **6.00 mm** (card plane). Buttons: 3.45 body / 5.0 actuator → **clear by
≥1.0 mm**. Nav = **8.00 mm → protrudes 2.0 mm above the card plane**: the card
must **not** overlap the nav footprint. This is the only front-side interference.

### Placement (worked out)

Art-centred on the active area:

* card (66.0 × 94.0) spans **x 120.66 → 186.66**, **y 70.35 → 164.35**.
* The card's left edge then overlaps the nav (right edge x 122.6) by **1.94 mm**,
  so **shift the card +1.94 mm right**: centre x → **155.60**. The art centre is
  then 1.94 mm right of the active centre — well inside the ±8 mm budget.
* **▸ Then shift a further +0.75 mm right**: centre x → **156.35**. The +1.94
  shift alone leaves the sleeve's left edge at x 122.60, *exactly* flush with
  the nav's right edge, and the nav protrudes 2 mm above the card plane. The
  ±8 mm art budget absorbs the extra shift easily.
* After the shift the right edge is 188.60, overlapping B1 (183.75–190.25) but
  not A1 (192.28+); B1 is below the card plane, so it does not matter.

### Overhang

* **Top: +1.75 mm** above the board top edge (essentially flush).
* **Bottom: 28.25 mm** below the board bottom edge.
* Bottom-heavy is accepted, so the card's top margin sits over the board and
  the top edge is only ~1.75 mm proud. The top cannot use a behind-the-card
  clamp in an overhang — it needs an **edge hook** (see design).

## Proposed design

* **Two bracket bars** fastened into the four M3 heat-set inserts with M3 bolts
  through the board holes. The **back plate is printed into each bar** (common,
  no separate shim), set coplanar with the 6.00 mm glass plane.
* **Bottom bar** (bolted at H4/H5): **a short bracket that stays on the board**,
  not a 28 mm cantilever. Its back plate rides on the two insert bosses, so the
  underside is at z = 3.6 mm and spans the LED row without touching it (see
  Front-side parts). **Two screw clamps** (Variant P point pad vs. Variant R
  short rail) press the card onto the plate at y ≈ 132.2 — blank card, well
  below the art window's lower edge at y ≈ 119.6, and the whole card width is
  art-free there. **No bottom-edge stop**: the card's 28 mm overhang simply
  hangs, which the placement already accepts.
* **Top bar** (bolted at H2/H3): a **raised bridge** (underside clear of the
  card plane and of SELECT/START) ending in a **hook that captures the card's
  top edge** — the ~1.75 mm overhang plus the card's ~13 mm top margin give a
  clean, art-free contact. The hook gives the top its registration; the bottom
  clamps provide the friction lock. Two spaced bottom clamps + the top hook
  constrain translation and rotation.
* **Clamp contact** stays in the card's outer margin, **outside the art box**
  (side bands ≈6 mm, top zone ≈13 mm, bottom margin ≈43 mm).
* **▸ Clamp screws are bought, not printed.** A **6 mm M3 heat-set insert** goes
  into an 8 mm-thick arm, bored from its top face; a **stock M3 × 8 mm socket-cap
  bolt** drives it. `arm_thk` equals the bolt's shank, so the tip lands exactly
  at the arm's underside, 0.3 mm above the cartridge — no trimming, no helix to
  model, and no PETG thread to creep. (Was: a printed PETG thumb thread.)
* **▸ Towers stand outside the card.** The sleeved card occupies x 120.66–186.66,
  y 70.35–164.35, so nothing above the card plane may sit inside it. Each tower
  is at x ≈ −41 / +45, with an arm reaching inward to a clamp axis at x ≈ −19.1 /
  +28.9 (model coords) — symmetric about the card centre and 48 mm apart, so two
  clamps resist card rotation. Towers reach z = 16.7 mm, which is the price of an
  8 mm arm plus an 8 mm stock bolt; trimming bolts would halve it.
* **▸ The clamp contact is below the art window, not beside it.** At the clamp Y
  the whole card width is blank, so the "outside the art box" test is a 2D one —
  the clamp only has to be outside the art *rectangle*, not outside its X range.
  That frees the axes to sit symmetrically, which is what makes a rail-length
  contact fit on the sleeve.
* **▸ Swappable cartridge** is a loose Ø14 × 2 mm disc. Its compliant pad stands
  0.2 mm below the rigid rim, so the rim bottoms on the card and bounds the
  squeeze at `clamp_stop_z` = 6.2 mm. Still to add: a spigot/dimple coupling to
  the bolt tip, and a printed thumb knob (a hex key drives v1).
* A loose **cradle** (bottom ledge + light side references) sets nominal
  placement; clamps take up the slack.
* **No side walls.** Slide clearance ~0.3–0.4 mm/side until the coupon says otherwise.

## Fabrication / material notes

* Board holes are **Ø3.00 mm** — line-to-line for an M3 shank. An M3 bolt passes
  and its head clears the back-side parts (already verified). If it binds, ream
  to ~3.2 mm (1/8" bit, pin vise, hand-turned) or step to M2.5.
* Print the **screw threads and flexures in PETG** (PLA creeps under sustained
  load and is brittle at the flexure); expect occasional re-snug.
* Print the screw axis vertical to the bed; coarse (~2.5–3 mm) trapezoidal thread.
* Print a **fit-check coupon** (two posts + cradle + card gauge + one clamp with
  both cartridges) before the full part.

## Build plan

1. **Reference geometry** — **done** (`cad/`, verified 2026-09-30). Note the
   Fusion MCP can neither import nor export DXF, so the board outline is
   **parsed from the `.kicad_pcb` text** by `cad/reference/kicad_extract.py`
   rather than exported to DXF and re-imported. Card and art window are modelled
   from the derived values; the active rectangle is at centre (153.66, 99.85).
   In the model, the origin is the board centre with **+Y up** (KiCad's y is
   negated) and z = 0 at the board front face.
2. **Coupon** — **emitted** as a separate body from the same build
   (`BRK_coupon`), so it cannot drift from the holder. Six insert-pilot bosses
   sweeping 4.0–4.5 mm.
3. **Placement** — **done.** Card at centre x 156.35, +0.75 mm of nav clearance.
4. **Model** — **done for the geometry as drawn.** Bottom bracket (plate on two
   Ø8 insert bosses, two clamp towers with arms, two cartridges, two thumb
   knobs) and top bar (bosses, raised bridge, edge hook). 7 bodies.
5. **A/B** — point pad vs. short rail cartridge — **open**; the coupon prints the
   bosses, the cartridges still need a rail variant to compare against.
6. **Full part** — print, install inserts, assemble. Add supports to the
   brackets' long horizontal plates (see Open items 5).

## Open items

Resolved by the CAD, checked against `cad/reference/board.json`:

* **Bolt-head back-side clearance.** All 128 B.Cu parts were tested against a
  Ø5.5 head plus 1 mm clearance at every hole. The tightest is H5 (I2C
  connector, 6.89 mm). No interference. Note the `conservative_radius` circle
  over-reports badly for long parts and false-alarmed on the AAA holder at H4
  (−2.06 mm); the rotated footprint rectangle gives 10.85 mm. Use the rectangle.
* **Tower placement** is now enforced by a build-time check: both towers stand
  outside the sleeve's X range, so nothing above the card plane touches the card.
* **Clamp contact** is checked to be on the sleeve and outside the art box.

Still open:

1. Confirm the art window against a flatbed scan before treating ≈51 × 33 as final.
2. Verify the top-hook grip depth against the card's R3.18 corner radius and the
   top-edge notch (x 151.70 → 158.10).
3. Confirm the SK6812MINI height. `neopixel.stp` says 0.75 mm, the common part is
   ~1.6 mm. `led_h` is set to the conservative 1.6 and the clearance is checked;
   the plate's 3.6 mm underside clears either by ≥2.0 mm.
4. Insert pilot diameter — the coupon sweeps 4.0–4.5 mm in 0.1 mm steps.
5. **Printability (unresolved).** Both brackets are horizontal plates carried on
   two bosses, so each has a ~100 mm unsupported span at its underside. Expect to
   need support material, or print on a different axis. A print-orientation
   decision is still outstanding.
6. **A/B:** point pad vs. short rail cartridge — **both are now modelled**
   (`BRK_cart_*` and `BRK_rail_*`), so the comparison is a print and a feel, not
   a redesign. Pick one per side after testing.
