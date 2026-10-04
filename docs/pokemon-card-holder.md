# SYCL Badge V2 — Pokémon Card Holder

**Status: designed, printed, and installed — 2026-10-04.**

The holder bolts to the SYCL Badge V2 and holds a penny-sleeved Pokémon card
flat against the LCD, with the card's art window aligned over the lit panel. The
design, geometry, and build script live in [`cad/`](../cad/). This document
describes the finished holder; the decisions that produced it are collected in
[Appendix A](#appendix-a--decision-log), and the measurements it rests on are in
[Appendix B](#appendix-b--measured-values).

## What it is

Two printed bars clamp a **penny-sleeved card** to the badge:

- a **bottom bracket** carrying two thumb-screw clamps that press the card onto
  a back plate at the card's blank lower margin, and
- a **top bar** whose raised bridge ends in a **hook** that captures the card's
  top edge.

The card is offered up by eye, its art window over the active area, then locked
by the two clamps; the top hook fixes the vertical position and the two spaced
clamps resist rotation. The card is held by **friction**, not forced into a fixed
orientation.

Both bars attach through the board's **four mounting holes** into **M3 heat-set
inserts** printed into the bars' bosses. Nothing on the holder touches the card's
art window, and nothing above the card plane intrudes on the sleeved card.

## The parts

All nine bodies are generated from one build script, `cad/fusion/build.py`.

| Body | Role | Key dimensions (mm) |
| --- | --- | --- |
| `BRK_bottom` | Bottom bracket: back plate on two insert bosses, a support rib, two clamp towers and arms, each arm with a tapped thumb-screw bore | plate 2.4 thick, underside z 3.6; bosses Ø8 × 3.6; rib 2 wide; arms z 8.7–16.7 |
| `BRK_top` | Top bar: insert bosses, a raised bridge, an edge hook, with **Ø9 finger openings over START and SELECT** | bridge z 6.8–9.8; hook down to z 5.0 |
| `BRK_cart_l`, `BRK_cart_r` | Clamp cartridges (installed): a compliant point pad standing in a rigid rim | Ø14 × 2; pad Ø6, standing 0.2 proud |
| `BRK_rail_l`, `BRK_rail_r` | A/B alternative: a short line-contact rail cartridge | pad 12 × 4 |
| `BRK_screw_l`, `BRK_screw_r` | Printed thumb screws: hex head, modelled M8x1.25 thread, flat tip | head 20 across flats × 6 |
| `BRK_coupon` | Fit-check coupon: six insert pilots (Ø4.0–4.5) and a thread gauge boss | 130 × 30 × 3 |

The bars and screws print in **PETG** — the screws because PETG resists creep
under the clamp load, and the cartridges' compliant pads likewise. The rails and
the coupon are optional prints.

## How it goes together

1. Print the parts. The tapped clamp bores print clean with the M8x1.25 thread
   (see [Appendix D](#appendix-d--print-notes)).
2. Press four **M3 × 6 mm heat-set inserts** into the four bosses from the
   **underside** (the board-facing face).
3. Bolt each bar through the board's mounting holes with **M3 × 8 mm** bolts and
   a **≈0.5 mm spacer** under each head.
4. Drop a cartridge at each clamp axis, thread in the thumb screws, offer the
   card up under the top hook, and tighten the clamps.

## Dimensions of the end state

Model coordinates: origin at the board centre, **+X right, +Y up**, z = 0 at the
board's front (component) face.

| Feature | Value |
| --- | --- |
| Card rest plane (LCD glass top) | z = 6.00 |
| Card stack top (`card_face_z`) | z = 6.40 |
| Hard clamp stop (`clamp_stop_z`) | z = 6.20 |
| Bottom back plate | z 3.6–6.0 (underside clears the LED row) |
| Clamp arm | z 8.7–16.7 |
| Clamp axes | x = −19.06 and +28.94, y = −25.80 — 48 apart, symmetric about the card |
| Cartridge / pad | Ø14 × 2 / Ø6 pad, 0.2 proud of the rim |
| Clamp thread | modelled **M8x1.25**, printed with ≈0.6 mm diametral slack |
| Thumb screw | 20 mm across flats, 6 mm tall, flat tip; 1.5 mm free travel at rest |
| Top bridge / hook | underside z 6.8; hook captures the card top edge (y ≈ 32.1–34.1, down to z 5.0) |
| START/SELECT openings | Ø9 through the bridge, centred on each switch, breaking its front edge |

## Placement

- The card is **art-window over active-area**. The vertical position is derived
  so the art box centres on the panel; the horizontal position is set by the
  **5-way nav** (the only front feature above the card plane), not by the art.
- The sleeved card is **art-free at the clamp line** — the clamp sits ~12.8 mm
  below the art window's lower edge, and the whole contact stays outside the art
  rectangle.
- The card's bottom hangs ~30 mm past the board edge; accepted. The top edge is
  essentially flush with the board, so it is captured by a hook rather than a
  behind-the-card clamp.

## The repository

The model is **a function of tracked text**, not a binary document.

| Path | Tracked | Role |
| --- | --- | --- |
| `cad/parameters.json` | yes | every dimension |
| `cad/fusion/build.py` | yes | the geometry, run inside Fusion |
| `cad/reference/board.json`, `cad/reference/kicad_extract.py` | yes | board geometry derived from the KiCad file |
| `cad/fusion/target.json` | yes | names the cloud Fusion document to build into |
| `cad/export/card-holder.step` | **yes** | the reviewed geometry (ASCII, diffable) |
| `cad/export/*.stl`, `*.3mf`, `*.f3d`, `*.png` | no | generated build products |

Fusion documents live in Autodesk's cloud and cannot be saved locally, so the
STEP is the one committed copy of the geometry; everything else is regenerated
on demand. Rebuild with:

```sh
make cad-ref     # regenerate cad/reference/board.json from the KiCad file
make cad-check   # assert the board matches the plan's data
make cad-build   # syntax-check and run the model's clearance checks
# then run cad/fusion/build.py inside Fusion (Utilities > Add-Ins > Scripts,
# or the Fusion MCP execute tool) to rebuild and re-export the snapshots
```

## Appendix A — Decision log

Chronological, including reversals.

1. **CAD tool and source of truth.** Fusion 360, driven entirely by
   `parameters.json` + `build.py`; the Fusion document is scratch state that can
   be deleted and rebuilt from the tracked text.
2. **Attachment.** Four M3 heat-set inserts in the printed bars; M3 bolts pass
   through the board's holes from the back. **6 mm inserts**, not 8 mm (8 mm
   would break through into the board); inserts load from the **underside**; a
   **0.5 mm spacer** under each head keeps the bolt tip inside the insert.
3. **Card orientation and retention.** Portrait; art-window over the active
   area; the bottom-heavy overhang accepted. Retention is a printed
   **thumb-screw friction clamp** plus a top **edge hook**; there is no
   bottom-edge stop.
4. **Placement derived from the art window**, not hand-placed: the card top is
   computed from the art window's centre, which is what makes all nine target
   cards land on the panel. The horizontal position is set by the nav keep-out.
5. **Symmetric clamp axes** (48 mm apart, outside the art box) so two clamps
   resist rotation; the towers stand outside the sleeve so nothing above the
   card plane can touch the card.
6. **Support rib under the plate** so its 106 mm span prints self-supporting.
7. **Printed clamp screw.** First a modelled TR8x1.5 trapezoidal thread; an
   intermediate revision mistakenly substituted an M3 insert + bolt and was
   reverted. The screw tip gained a centring **spigot**, which was later
   **removed for a flat tip** (user request).
8. **Rails vs pads.** Both cartridge variants were modelled (point pad and short
   rail) so the choice is a print-and-feel test, not a redesign. The rails are
   parked off the assembly.
9. **Top bridge and buttons.** The bridge must ride above the card plane to
   clear START/SELECT; **Ø9 finger openings** were cut through it over each
   switch so both stay visible and pressable.
10. **2026-10-04 — support filling the clamp bores.** Bambu Studio filled the
    tapped bores with support. The trigger was the **TR8x1.5 thread's 15° flank**
    (measured: 221.7 mm² of downward face inside the bores, slope 15–16°), and
    the support threshold cannot remove it (Bambu: a larger threshold angle
    generates *more* support). The thread was **re-profiled to ISO Metric
    M8x1.25** (30° flank), which drops the bore's sub-threshold area to ≈0.2 mm².
    Printed and installed successfully.

## Appendix B — Measured values

Calipers, 2026-09-30.

| # | Measurement | Nominal | Actual |
| --- | --- | --- | --- |
| 1–4 | Sleeved / bare card W × H | 66.7 × 92.1 / 63.5 × 88.9 | **66.0 × 94.0 / 63.0 × 87.5** |
| 5 | Stack thickness (card + sleeve) | ~0.39 | ~0.40 |
| 6–8 | Art window W × H and insets | derive | **≈57 × 38.5** keep-out (with 1 mm margin) |
| 10–11 | LCD active W × H | 28.03 × 35.04 | **34.5 × 27.5** (landscape) |
| 13 | Glass height above board | ~2.64 | **6.00 (confirmed)** |
| 14–15 | Board thickness / hole bore | 1.60 / 3.00 | 1.60 / 3.00 |
| 16–17 | Nav / tact heights | — | 8.00 / 3.45 body, 5.0 actuator |
| 18–20 | Insert barrel OD / flange / length | — | Ø5.0 / **none** / 6 or 8 |
| 21–23 | Bolt lengths / head / pitch | — | 8/12/16/20 / Ø5.0 × ~3.0 / 0.5 |

Board: **110.007 × 64.015 mm**, corner R3.0 nominal (the drawn arcs run
R3.00–R3.12), centre (151.410, 104.099) in KiCad coordinates. The four mounting
holes are **not a perfect rectangle**; the model uses the exact per-hole XY in
`cad/reference/board.json`.

## Appendix C — Open items

- **A/B cartridge:** point pad vs. short rail — pick one per side after a feel
  test.
- **Art window:** measured off scans with a 1 mm margin; a flatbed scan would let
  the margin tighten.
- **Coupon:** re-print it against the new M8x1.25 thread to confirm the printed
  thread fit before relying on it.
- **Top hook:** confirm the grip depth against the card's corner radius and the
  top-edge notch.

## Appendix D — Print notes

- **Threads and flexures in PETG.** Print the screw axis vertical. PLA creeps
  under sustained load and is brittle at the flexure.
- **Support.** The bottom bracket prints self-supporting except for the clamp
  arms, and the top bar's bridge needs support. The clamp bores must **not** be
  filled: the M8x1.25 thread is self-supporting, so any support reaching the
  bore means the thread or the threshold is off — the guaranteed fallback is a
  correctly-sized **support blocker** (Ø11–14 mm, spanning the arm) over each
  bore.
- **Coupon first.** The coupon prints six insert pilots (Ø4.0–4.5) and a thread
  gauge; use them before committing the holder.
