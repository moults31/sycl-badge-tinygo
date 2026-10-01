# Card holder CAD

The 3D-printed holder that clips a penny-sleeved Pokémon card flat onto the SYCL
Badge V2 LCD, so the panel shines through the card's art window.

Design of record: [`../docs/pokemon-card-holder-plan.md`](../docs/pokemon-card-holder-plan.md).

## Source of truth

The model is a **function of tracked text**, not a binary document. In
particular, `cad/fusion/card-holder.f3d` is a scratch working file: it is
gitignored, it is never read as data, and it can be thrown away and rebuilt.

| Path | Tracked | Role |
| --- | --- | --- |
| `parameters.json` | yes | **Every dimension.** Named user parameters, expressions allowed. |
| `reference/kicad_extract.py` | yes | Derives board geometry from the KiCad file. Text in, text out. |
| `reference/board.json` | yes | Generated: board outline, mounting holes, provenance hash. |
| `fusion/build.py` | yes | **The geometry.** Runs inside Fusion; rebuilds the model from the two files above. |
| `fusion/target.json` | yes | Which cloud document to build into. Just a name; the document itself is scratch. |
| `export/*.step` | **yes** | The reviewed geometry. ASCII, diffable, needs neither Fusion nor an Autodesk account. |
| `export/*.stl`, `*.3mf`, `*.f3d` | no | Slicing products and an archive snapshot. Regenerated every build. |

Everything above the `fusion/` line can be reviewed in a diff. That is the point.

## There is no local document

Fusion documents live in Autodesk's cloud. There is no "Save As to this
computer", `save_as` on Fusion's execute schema is undocumented and just re-sends
`save`, and Fusion refuses to save an Untitled document programmatically — the
first save is a UI action. The only local write is an **export snapshot**.

So the document is **scratch state**, not an asset:

- `cad/fusion/target.json` names it. `build.py` refuses to run unless the active
  document's name matches, so a rebuild cannot land in the wrong document.
- The document can be deleted and rebuilt from the tracked text at any time.
- If it drifts from the script, the script wins: `build.py` clears its own
  sketches and redraws them rather than editing what it finds.
- It is shared globally, so do not build into it from two worktrees at once.

This makes the "rebuild from a blank document" property load-bearing rather than
a convenience. That is fine, and it is the honest version of the intent: Fusion
is a renderer we can throw away.

Local outputs are therefore only ever snapshots, written to `export/`. The STEP
is committed on purpose: it is ASCII and diffable, and it is the one copy of the
geometry that does not require Fusion *and* an Autodesk account. STL/3MF and the
`.f3d` snapshot are gitignored build products.

## Coordinate system

```
origin  board centre
+X      right                   (KiCad +x)
+Y      up                      (KiCad -y -- KiCad's y grows downward)
Z = 0   board front face (F.Cu) the LCD / card side
+Z      out toward the card
```

`reference/board.json` keeps KiCad's own coordinates verbatim (provenance); the
KiCad-to-model transform lives in `fusion/build.py` and is checked by
`--check`, which reproduces the plan's placement numbers independently of CAD.

## Workflows

```sh
make cad-ref       # regenerate reference/board.json from the hardware repo
make cad-check     # ...and assert it is the board the plan describes
make cad-build     # syntax-check the Fusion build script, print resolved geometry
```

`make cad-ref` needs no Fusion and no KiCad. It reads the s-expressions in
`SYCL Badge 2024.kicad_pcb` directly, so a board revision shows up as a diff in
`board.json`. Point it elsewhere with `make cad-ref CAD_BOARD=/path/to.kicad_pcb`,
or set `$SYCL_BOARD_PCB`.

Building the actual model needs Fusion. `cad/fusion/build.py` runs inside
Fusion (Utilities > Add-Ins > Scripts, or the fusion MCP `execute` tool); it
syncs the parameters, clears its own sketches, and redraws them. Re-running it
is always safe. To run it outside Fusion, `python3 cad/fusion/build.py --check`
evaluates the parameter expressions and prints the model-space geometry with no
CAD involved.

## What the build produces

Nine bodies in one document, all regenerated from the tracked text:

| body | what |
| --- | --- |
| `BRK_bottom` | back plate on two Ø8 insert bosses at H4/H5, two clamp towers with arms |
| `BRK_cart_l` / `BRK_cart_r` | point-pad cartridges, Ø14 × 2, compliant pad 0.2 mm proud of the rigid rim |
| `BRK_rail_l` / `BRK_rail_r` | rail cartridges, the A/B alternative: a line contact instead of a point |
| `BRK_knob_l` / `BRK_knob_r` | printed thumb knobs, hex pocket gripping the bolt head |
| `BRK_top` | top bar at H2/H3: bosses, raised bridge, edge hook |
| `BRK_coupon` | fit-check coupon: six insert-pilot bosses sweeping 4.0–4.5 mm |

Nine bodies: the two cartridges and the two knobs are per-side parts, and the
pad and rail cartridges are alternatives. The pad cartridges sit at the clamp
axes (installed); the rails are **parked at y = −70 mm** with the coupon, so no
two bodies share a space and a slicer can take any subset. `analyzeInterference`
reports 0 overlapping pairs across all nine.

`build.py` also runs clearance checks against the extracted board data — nav,
LED, LCD module, tower/art-box placement, hook capture, bolt-tip depth, and
bolt-head space against all 128 back-side parts. They run in `--check` and again
inside Fusion, so they cannot rot.

## Rules this directory follows
1. **No hand-drawn geometry.** Every edge traces to `parameters.json` or to
   `reference/board.json`. Nothing is dimensioned by eye in the Fusion UI.
2. **No unnamed numbers in sketches.** Dimensions read parameters.
3. **The board file is the board.** Nominal values from the plan are identified
   as such; where the source disagrees, the source wins and the disagreement is
   printed, not rounded away. See the notes from `make cad-check` (the drawn
   corners are R3.00–R3.12, not a clean R3.0).
4. **No cloud documents.** The working `.f3d` is a local file.
5. **No unchecked-in build steps.** If a step matters, it lives in `build.py`.

## Parameter status

Each parameter is `verified` (traced to a measurement or the hardware repo) or
`provisional` (a starting point to tune on the coupon). Provisional values are
tagged `[PROVISIONAL]` in the comment that reaches Fusion, so they are visible
in Fusion's own parameter table rather than only in this repo.

The provisional set is the clamp design itself: thread, flexure, and the
structural bar dimensions. Those get settled by printing
`docs/pokemon-card-holder-plan.md`'s fit-check coupon.
