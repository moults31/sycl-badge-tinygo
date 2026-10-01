#!/usr/bin/env python3
"""Build the SYCL Badge V2 card holder in Fusion from tracked inputs.

The model is a function of two tracked files:

    cad/parameters.json        every dimension, as named user parameters
    cad/reference/board.json   board outline + mounting holes, derived from the
                               hardware repo's KiCad file by kicad_extract.py

This script -- not the .f3d -- is the source of truth for the geometry. It is
idempotent: it syncs the parameters, deletes and rebuilds its own named sketches
and bodies, and can be re-run on the same document any number of times. It never
treats the Fusion document as data.

Fusion documents live in Autodesk's cloud: there is no local live .f3d and no
Save As to disk. The document is therefore scratch state, and
cad/fusion/target.json names it. This script refuses to run unless the active
document's name matches, so a rebuild cannot land in the wrong document.

Two ways to run it:

    # outside Fusion: re-evaluate the parameters and print the model-space
    # geometry. Reviews the arithmetic with no CAD involved.
    python3 cad/fusion/build.py --check

    # inside Fusion: Utilities > Add-Ins > Scripts, or the fusion MCP `execute`
    # tool (paste this file as the script). Fusion calls run(_ctx).

Coordinate system (see cad/README.md):

    origin  board centre
    +X      right                       (KiCad +x)
    +Y      up                          (KiCad -y; KiCad's y grows downward)
    Z = 0   board front face (F.Cu)     the LCD / card side
    +Z      out toward the card
"""

from __future__ import annotations

import json
import math
import os
import re
import sys
from pathlib import Path

CM_PER_MM = 0.1
MM_PER_CM = 10.0

SKETCH_PREFIX = "REF_"
PLANE_GLASS = "PL_glass"

BODY_PREFIX = "BRK_"
PLANE_HOLDER = "PL_brk_"


# ---------------------------------------------------------------- repo / inputs

def find_repo() -> Path:
    """Locate the checkout that holds cad/. Override with $SYCL_REPO."""
    env = os.environ.get("SYCL_REPO")
    if env:
        return Path(env).expanduser().resolve()

    here = globals().get("__file__")
    if here:
        return Path(here).resolve().parents[2]

    # Executed through Fusion's `execute`, which defines no __file__.
    for c in (Path.home() / "code" / "sycl_land" / "sycl-badge-tinygo", Path.cwd()):
        if (c / "cad" / "parameters.json").is_file():
            return c.resolve()
    raise RuntimeError("cannot locate the repo; set SYCL_REPO to its root")


def load_inputs(repo: Path):
    params = json.loads((repo / "cad" / "parameters.json").read_text())
    ref_path = repo / "cad" / "reference" / "board.json"
    if not ref_path.is_file():
        raise RuntimeError(f"{ref_path} missing -- run: make cad-ref")
    ref = json.loads(ref_path.read_text())
    return params, ref


def load_target(repo: Path) -> dict:
    """Which cloud document to build into (see cad/fusion/target.json)."""
    p = repo / "cad" / "fusion" / "target.json"
    return json.loads(p.read_text()) if p.is_file() else {}


def param_defs(params_doc: dict) -> list[dict]:
    """Flatten the sectioned parameter table into Fusion's add_parameters shape."""
    defs = []
    for section, entries in params_doc["sections"].items():
        for e in entries:
            status = e.get("status", "verified")
            comment = e.get("comment", "")
            if status != "verified":
                comment = f"{comment} [{status.upper()}]" if comment else status.upper()
            defs.append({
                "name": e["name"],
                "expression": e["expression"],
                "units": params_doc.get("units", "mm"),
                "comment": f"[{section}] {comment}".strip(),
            })
    return defs


_SAFE_EXPR = re.compile(r"^[0-9A-Za-z_\.\s\+\-\*/\(\)]+$")


def eval_params(defs: list[dict]) -> dict[str, float]:
    """Evaluate the parameter expressions out of Fusion, in source order.

    Only arithmetic on earlier parameters is allowed; the unit suffix is
    stripped because the document's unit is already mm.
    """
    values: dict[str, float] = {}
    for d in defs:
        expr = re.sub(r"\b(mm|deg|cm)\b", "", d["expression"]).strip()
        if not expr:
            raise ValueError(f"{d['name']}: empty expression")
        if not _SAFE_EXPR.match(expr):
            raise ValueError(f"{d['name']}: expression {d['expression']!r} is not plain arithmetic")
        try:
            values[d["name"]] = float(eval(expr, {"__builtins__": {}}, dict(values)))  # noqa: S307
        except NameError as exc:
            raise ValueError(f"{d['name']}: unknown parameter in {d['expression']!r} ({exc})") from None
    return values


# ------------------------------------------------------------------- geometry

def _crect(cx, cy, w, h):
    return {"x": (cx - w / 2.0, cx + w / 2.0), "y": (cy - h / 2.0, cy + h / 2.0)}


def _rect_top(cx, top_y, w, h):
    return {"x": (cx - w / 2.0, cx + w / 2.0), "y": (top_y - h, top_y)}


def _keepout(fp: dict, m, cx: float, cy: float) -> dict:
    """A footprint keep-out in model coordinates: both a circle and a rectangle.

    The rectangle is the right primitive for bolt-head space; the circle is kept
    because it is the conservative one for irregular parts.
    """
    x0, y0, x1, y1 = fp["keepout_rect"]
    return {
        "ref": fp["ref"], "value": fp["value"], "pads": fp.get("pads", 0),
        "center": m(fp["at"]), "radius": fp["conservative_radius"],
        "rect": (min(x0 - cx, x1 - cx), min(cy - y0, cy - y1),
                 max(x0 - cx, x1 - cx), max(cy - y0, cy - y1)),
    }


def geom(ref: dict, v: dict[str, float]) -> dict:
    """Everything the build draws, in model coordinates (mm)."""
    bb = ref["bbox"]
    cx = (bb["x"][0] + bb["x"][1]) / 2.0
    cy = (bb["y"][0] + bb["y"][1]) / 2.0

    def m(p):
        return (p[0] - cx, cy - p[1])

    outline = []
    for s in ref["outline"]:
        seg = {"kind": s["kind"], "start": m(s["start"]), "end": m(s["end"])}
        if "mid" in s:
            seg["mid"] = m(s["mid"])
        outline.append(seg)

    holes = [{"ref": h["ref"], "at": m(h["at"]), "drill": h["drill"]} for h in ref["mounting_holes"]]

    board = {"x": (bb["x"][0] - cx, bb["x"][1] - cx), "y": (cy - bb["y"][1], cy - bb["y"][0])}
    sleeve = _rect_top(v["card_cx"], v["card_top_y"], v["sleeve_w"], v["sleeve_h"])
    card = _rect_top(v["card_cx"], v["card_top_y"] - v["sleeve_margin_y"], v["card_w"], v["card_h"])
    art = {
        "x": (card["x"][0] + v["art_inset_l"], card["x"][1] - v["art_inset_r"]),
        "y": (card["y"][1] - v["art_inset_t"] - v["art_h"], card["y"][1] - v["art_inset_t"]),
    }
    active = _crect(v["active_cx"], v["active_cy"], v["active_w"], v["active_h"])

    keepouts = [_keepout(fp, m, cx, cy) for fp in ref.get("front_footprints", [])]
    back_keepouts = [_keepout(fp, m, cx, cy) for fp in ref.get("back_footprints", [])]

    return {
        "origin_kicad": (cx, cy),
        "outline": outline,
        "holes": holes,
        "board": board,
        "sleeve": sleeve,
        "card": card,
        "art": art,
        "active": active,
        "front_keepouts": keepouts,
        "back_keepouts": back_keepouts,
        "z": {"board_face": 0.0, "glass": v["glass_z"], "card_face": v["card_face_z"],
              "clamp_stop": v["clamp_stop_z"]},
    }


def _fmt_rect(r):
    return (f"x {r['x'][0]:+8.3f} .. {r['x'][1]:+8.3f}  "
            f"y {r['y'][0]:+8.3f} .. {r['y'][1]:+8.3f}  "
            f"({r['x'][1] - r['x'][0]:.3f} x {r['y'][1] - r['y'][0]:.3f})")


def report(g: dict, v: dict[str, float]) -> str:
    lines = []
    lines.append(f"origin (KiCad coords): {g['origin_kicad'][0]:.4f}, {g['origin_kicad'][1]:.4f}")
    lines.append(f"board   {_fmt_rect(g['board'])}")
    lines.append(f"sleeve  {_fmt_rect(g['sleeve'])}")
    lines.append(f"card    {_fmt_rect(g['card'])}")
    lines.append(f"art     {_fmt_rect(g['art'])}   <- clamp keep-out")
    lines.append(f"active  {_fmt_rect(g['active'])}")
    lines.append("mounting holes (model coords):")
    for h in g["holes"]:
        lines.append(f"  {h['ref']}  x {h['at'][0]:+8.3f}  y {h['at'][1]:+8.3f}  D{h['drill']:.2f}")
    z = g["z"]
    lines.append(f"planes: board_face z={z['board_face']:.2f}  glass z={z['glass']:.2f}  "
                 f"card_face z={z['card_face']:.2f}  clamp_stop z={z['clamp_stop']:.2f}")
    lines.append(f"outline segments: {len(g['outline'])} "
                 f"({sum(1 for s in g['outline'] if s['kind'] == 'arc')} arcs)")
    return "\n".join(lines)


# --------------------------------------------------------------- fusion build

def _norm_expr(s) -> str:
    """Fusion reformats expressions on write, e.g. '(sleeve_w - card_w) / 2'
    comes back as '( sleeve_w - card_w ) / 2'. Compare ignoring whitespace, or
    every rebuild rewrites those parameters and re-dirties the document."""
    return re.sub(r"\s+", "", s or "")


def sync_parameters(design, defs: list[dict]):
    """Idempotent: add missing, update changed. Returns (added, updated, total)."""
    import adsk.core

    ups = design.userParameters
    added = updated = 0
    for d in defs:
        p = ups.itemByName(d["name"])
        if p is None:
            ups.add(d["name"], adsk.core.ValueInput.createByString(d["expression"]),
                    d["units"], d["comment"])
            added += 1
        elif _norm_expr(p.expression) != _norm_expr(d["expression"]):
            p.expression = d["expression"]
            p.comment = d["comment"]
            updated += 1
    return added, updated, ups.count


def _read_params(design, names) -> dict[str, float]:
    ups = design.userParameters
    out = {}
    for n in names:
        p = ups.itemByName(n)
        if p is None:
            raise RuntimeError(f"parameter {n!r} is missing after sync")
        out[n] = p.value * MM_PER_CM
    return out


def _pt(x_mm, y_mm, z_mm=0.0):
    import adsk.core
    return adsk.core.Point3D.create(x_mm * CM_PER_MM, y_mm * CM_PER_MM, z_mm * CM_PER_MM)


def _fresh_sketch(root, name, plane):
    sk = root.sketches.add(plane)
    sk.name = name
    return sk


def _clear_reference(root) -> int:
    """Delete everything this script owns. Sketches first: a sketch sits on
    PL_glass, so the plane cannot go until the sketches that use it are gone."""
    removed = 0
    for i in range(root.sketches.count - 1, -1, -1):
        sk = root.sketches.item(i)
        if sk.name.startswith(SKETCH_PREFIX):
            sk.deleteMe()
            removed += 1
    plane = root.constructionPlanes.itemByName(PLANE_GLASS)
    if plane:
        plane.deleteMe()
    return removed


def _construction(sk):
    for c in sk.sketchCurves:
        c.isConstruction = True


def _xy_rect(sk, r):
    (x0, x1), (y0, y1) = r["x"], r["y"]
    lines = sk.sketchCurves.sketchLines
    lines.addByTwoPoints(_pt(x0, y0), _pt(x1, y0))
    lines.addByTwoPoints(_pt(x1, y0), _pt(x1, y1))
    lines.addByTwoPoints(_pt(x1, y1), _pt(x0, y1))
    lines.addByTwoPoints(_pt(x0, y1), _pt(x0, y0))


def _glass_plane(root, z_mm):
    import adsk.core

    old = root.constructionPlanes.itemByName(PLANE_GLASS)
    if old:
        old.deleteMe()
    inp = root.constructionPlanes.createInput()
    inp.setByOffset(root.xYConstructionPlane, adsk.core.ValueInput.createByString(f"{z_mm} mm"))
    pl = root.constructionPlanes.add(inp)
    pl.name = PLANE_GLASS
    return pl


def build_reference(root, g: dict) -> list[str]:
    """Draw the derived reference geometry. Nothing here is a solid."""
    removed = _clear_reference(root)
    made = []

    sk = _fresh_sketch(root, SKETCH_PREFIX + "board_outline", root.xYConstructionPlane)
    sk.isComputeDeferred = True
    lines = sk.sketchCurves.sketchLines
    arcs = sk.sketchCurves.sketchArcs
    for s in g["outline"]:
        a, b = s["start"], s["end"]
        if s["kind"] == "line":
            lines.addByTwoPoints(_pt(*a), _pt(*b))
        else:
            arcs.addByThreePoints(_pt(*a), _pt(*b), _pt(*s["mid"]))
    sk.isComputeDeferred = False
    _construction(sk)
    made.append(sk.name)

    sk = _fresh_sketch(root, SKETCH_PREFIX + "mounting_holes", root.xYConstructionPlane)
    circles = sk.sketchCurves.sketchCircles
    for h in g["holes"]:
        circles.addByCenterRadius(_pt(*h["at"]), (h["drill"] / 2.0) * CM_PER_MM)
    _construction(sk)
    made.append(sk.name)

    plane = _glass_plane(root, g["z"]["glass"])

    sk = _fresh_sketch(root, SKETCH_PREFIX + "active_area", plane)
    _xy_rect(sk, g["active"])
    _construction(sk)
    made.append(sk.name)

    sk = _fresh_sketch(root, SKETCH_PREFIX + "card_outline", plane)
    sk.isComputeDeferred = True
    _xy_rect(sk, g["sleeve"])
    _xy_rect(sk, g["card"])
    sk.isComputeDeferred = False
    _construction(sk)
    made.append(sk.name)

    sk = _fresh_sketch(root, SKETCH_PREFIX + "art_window", plane)
    _xy_rect(sk, g["art"])
    _construction(sk)
    made.append(sk.name)

    return made, removed


def _offset_plane(root, name, z_mm):
    import adsk.core

    old = root.constructionPlanes.itemByName(name)
    if old:
        old.deleteMe()
    inp = root.constructionPlanes.createInput()
    inp.setByOffset(root.xYConstructionPlane, adsk.core.ValueInput.createByString(f"{z_mm} mm"))
    pl = root.constructionPlanes.add(inp)
    pl.name = name
    return pl


def _glass_plane(root, z_mm):
    return _offset_plane(root, PLANE_GLASS, z_mm)


def _profiles(sk, largest_only=False):
    import adsk.core

    profiles = [sk.profiles.item(i) for i in range(sk.profiles.count)]
    if largest_only:
        # A sketch with two concentric circles yields the inner disc (one loop)
        # and the annulus around it (two loops). Loop count is the reliable
        # discriminator; Profile has no .area.
        profiles = [max(profiles, key=lambda p: p.profileLoops.count)]
    coll = adsk.core.ObjectCollection.create()
    for p in profiles:
        coll.add(p)
    return coll


def _extrude(root, sk, operation, distance_mm, name, largest_only=False):
    import adsk.core

    ex = root.features.extrudeFeatures
    inp = ex.createInput(_profiles(sk, largest_only), operation)
    inp.setDistanceExtent(False, adsk.core.ValueInput.createByString(f"{distance_mm} mm"))
    feat = ex.add(inp)
    feat.name = name
    return feat


def _hex(sk, cx, cy, across_flats):
    """A regular hexagon drawn as six lines; circumradius = across_flats / sqrt(3)."""
    r = across_flats / math.sqrt(3.0)
    pts = [(cx + r * math.cos(math.radians(60 * i)),
            cy + r * math.sin(math.radians(60 * i))) for i in range(6)]
    lines = sk.sketchCurves.sketchLines
    for i in range(6):
        lines.addByTwoPoints(_pt(*pts[i]), _pt(*pts[(i + 1) % 6]))


def _clear_holder(root) -> int:
    """Delete the holder's features, then its sketches, then its plane.

    Features first: deleting a feature takes the body it produced with it, so no
    orphan body is left to be re-created on the next run.
    """
    removed = 0
    for i in range(root.features.count - 1, -1, -1):
        f = root.features.item(i)
        if f.name.startswith(BODY_PREFIX):
            f.deleteMe()
            removed += 1
    for i in range(root.sketches.count - 1, -1, -1):
        if root.sketches.item(i).name.startswith(BODY_PREFIX):
            root.sketches.item(i).deleteMe()
            removed += 1
    for i in range(root.bRepBodies.count - 1, -1, -1):
        b = root.bRepBodies.item(i)
        if b.name.startswith(BODY_PREFIX):
            b.deleteMe()
            removed += 1
    for i in range(root.constructionPlanes.count - 1, -1, -1):
        p = root.constructionPlanes.item(i)
        if p.name.startswith(PLANE_HOLDER):
            p.deleteMe()
            removed += 1
    return removed


def checks(g: dict, v: dict) -> list[str]:
    """The clearances the design depends on, as explicit assertions.

    These are the ones that would fail silently otherwise: the model would look
    right and the part would not fit. They run in --check and again inside
    Fusion, so they cannot rot.
    """
    bad: list[str] = []

    def need(label, ok, detail):
        if not ok:
            bad.append(f"{label}: {detail}")

    sleeve_x = g["sleeve"]["x"]
    art_x = g["art"]["x"]

    nav = next((f for f in g.get("front_keepouts", []) if f["ref"] == "U3"), None)
    if nav:
        gap = sleeve_x[0] - (nav["center"][0] + nav["radius"])
        need("nav clearance", gap >= 0.5,
             f"sleeve left edge {sleeve_x[0]:.2f} is only {gap:.2f} mm from the nav keep-out")

    plate_under = v["glass_z"] - v["backplate_thk"]
    need("LED clearance", plate_under - v["led_h"] >= v["led_clearance"],
         f"plate underside z={plate_under:.2f} leaves {plate_under - v['led_h']:.2f} mm "
         f"over a {v['led_h']:.2f} mm LED")

    mod_bot = v["active_cy"] - v["lcd_h"] / 2.0
    need("LCD module clearance", v["bar_y1"] <= mod_bot - 1.0,
         f"bracket front edge y={v['bar_y1']:.2f} vs module lower edge {mod_bot:.2f}")

    for label, tower_x in (("left", v["tower_x_l"]), ("right", v["tower_x_r"])):
        clear = (tower_x + v["tower_w"] / 2.0 <= sleeve_x[0]
                 or tower_x - v["tower_w"] / 2.0 >= sleeve_x[1])
        need(f"{label} tower clear of card", clear,
             f"tower x={tower_x:.2f} overlaps the sleeve x {sleeve_x[0]:.2f}..{sleeve_x[1]:.2f}")

    for label, cx in (("left", v["clamp_x_l"]), ("right", v["clamp_x_r"])):
        need(f"{label} clamp on the sleeve", sleeve_x[0] < cx < sleeve_x[1],
             f"clamp x={cx:.2f} is off the sleeve")
        clamp_y = (v["bar_y0"] + v["bar_y1"]) / 2.0
        inside_art = (art_x[0] < cx < art_x[1]) and (g["art"]["y"][0] < clamp_y < g["art"]["y"][1])
        need(f"{label} clamp outside the art box", not inside_art,
             f"clamp ({cx:.2f}, {clamp_y:.2f}) is inside the art window "
             f"x {art_x[0]:.2f}..{art_x[1]:.2f} y {g['art']['y'][0]:.2f}..{g['art']['y'][1]:.2f}")

    for label, cx in (("left", v["clamp_x_l"]), ("right", v["clamp_x_r"])):
        half = v["rail_body_len"] / 2.0
        need(f"{label} rail stays on the sleeve",
             sleeve_x[0] <= cx - half and cx + half <= sleeve_x[1],
             f"rail spans {cx - half:.2f}..{cx + half:.2f} vs sleeve {sleeve_x[0]:.2f}..{sleeve_x[1]:.2f}")

    bridge_bot = v["card_face_z"] + v["bridge_clear"]
    need("bridge clears the tacts", bridge_bot > v["tact_actuator_z"],
         f"bridge underside {bridge_bot:.2f} vs tact actuator {v['tact_actuator_z']:.2f}")

    need("hook captures the card", v["hook_z"] < v["glass_z"],
         f"hook bottom z={v['hook_z']:.2f} does not dip below the card plane {v['glass_z']:.2f}")
    need("hook clears the card edge", v["hook_clear"] > 0.0, "hook_clear must be positive")

    # The bolts pass through from the back, so the head and its spacer need clear
    # space there. Checked against the rotated footprint rectangles, not the
    # circles: the AAA holder's circle claims 32 mm of radius and false-alarms.
    head_r = v["m3_head_d"] / 2.0 + 1.0
    for h in g["holes"]:
        hx, hy = h["at"]
        for k in g.get("back_keepouts", []):
            x0, y0, x1, y1 = k["rect"]
            dx = max(x0 - hx, 0.0, hx - x1)
            dy = max(y0 - hy, 0.0, hy - y1)
            dist = math.hypot(dx, dy)
            need(f"bolt head space at {h['ref']}", dist >= head_r,
                 f"{k['ref']} ({k['value']}) leaves {dist:.2f} mm; the head needs {head_r:.2f}")

    mount_tip = v["bolt_shank"] - v["board_thk"] - v["bolt_spacer"]
    need("mounting bolt tip stays inside the insert", 0.0 < mount_tip < v["insert_len"],
         f"tip z={mount_tip:.2f} vs insert 0..{v['insert_len']:.2f} "
         f"(shank {v['bolt_shank']:.2f}, board {v['board_thk']:.2f}, spacer {v['bolt_spacer']:.2f})")

    arm_top = v["card_face_z"] + v["cartridge_h"] + v["arm_clear"] + v["arm_thk"]
    cart_top = v["card_face_z"] + v["cartridge_h"]
    clamp_tip = arm_top - v["bolt_shank"]
    need("clamp bolt tip does not preload the cartridge", clamp_tip >= cart_top,
         f"tip z={clamp_tip:.2f} vs cartridge top {cart_top:.2f}")
    need("clamp bolt reaches through the insert", clamp_tip <= arm_top - v["insert_len"],
         f"tip z={clamp_tip:.2f} vs insert bottom z={arm_top - v['insert_len']:.2f}")

    return bad


def _centring_dimple(root, tag, cx, cy, v, top_z):
    """A shallow pocket in a cartridge's top face to centre the bolt tip."""
    from adsk.fusion import FeatureOperations as FO

    sk = _fresh_sketch(root, BODY_PREFIX + "dimple_" + tag,
                       _offset_plane(root, PLANE_HOLDER + "dimple" + tag,
                                     top_z - v["dimple_depth"]))
    sk.sketchCurves.sketchCircles.addByCenterRadius(
        _pt(cx, cy), (v["m3_shank_d"] / 2.0 + 0.1) * CM_PER_MM)
    _extrude(root, sk, FO.CutFeatureOperation, v["dimple_depth"] + 0.1,
             BODY_PREFIX + "dimple" + tag + "_cut")


def build_holder(root, g: dict, v: dict) -> dict:
    """The bottom bracket: a back plate carried on the two insert bosses.

    Its underside lands at glass_z - backplate_thk = 3.6 mm. That is what keeps
    it clear of the LED row at Y ~ -28 with no relief pockets: the plate never
    comes near the board, only the bosses do.
    """
    from adsk.fusion import FeatureOperations as FO

    hole = {h["ref"]: h for h in g["holes"]}
    half_x = v["board_w"] / 2.0 - v["bar_inset"]
    boss_od = v["insert_od"] + 2.0 * v["boss_wall"]
    boss_h = v["glass_z"] - v["backplate_thk"]

    # 1. the back plate, on a plane at the boss height, extruded up to the card plane
    plane = _offset_plane(root, PLANE_HOLDER + "plate", boss_h)
    sk = _fresh_sketch(root, BODY_PREFIX + "plate", plane)
    _xy_rect(sk, {"x": (-half_x, half_x), "y": (v["bar_y0"], v["bar_y1"])})
    feat = _extrude(root, sk, FO.NewBodyFeatureOperation, v["backplate_thk"], BODY_PREFIX + "plate_ext")
    body = feat.bodies.item(0)
    body.name = BODY_PREFIX + "bottom"

    # 2. the bosses, joining the plate's underside down to the board face
    sk = _fresh_sketch(root, BODY_PREFIX + "bosses", root.xYConstructionPlane)
    circles = sk.sketchCurves.sketchCircles
    for ref in ("H4", "H5"):
        circles.addByCenterRadius(_pt(*hole[ref]["at"]), (boss_od / 2.0) * CM_PER_MM)
    _extrude(root, sk, FO.JoinFeatureOperation, boss_h, BODY_PREFIX + "bosses_ext")

    # 3. the insert pilots, through from the board face (inserts load from below)
    sk = _fresh_sketch(root, BODY_PREFIX + "pilots", root.xYConstructionPlane)
    circles = sk.sketchCurves.sketchCircles
    for ref in ("H4", "H5"):
        circles.addByCenterRadius(_pt(*hole[ref]["at"]), (v["insert_pilot_d"] / 2.0) * CM_PER_MM)
    _extrude(root, sk, FO.CutFeatureOperation, v["glass_z"] + 1.0, BODY_PREFIX + "pilots_cut")

    # --- clamp towers, arms and cartridges -----------------------------------
    #
    # Each tower stands OUTSIDE the card's X range, because nothing above the
    # card plane may intersect the card; its arm then reaches inward over the
    # card's blank lower margin. arm_thk equals the kit's 8 mm bolt shank, so a
    # stock bolt's tip lands exactly at the arm's underside.
    cart_top = v["card_face_z"] + v["cartridge_h"]
    arm_bot = cart_top + v["arm_clear"]
    arm_top = arm_bot + v["arm_thk"]
    plate_y = (v["bar_y0"], v["bar_y1"])
    clamp_y = sum(plate_y) / 2.0
    half_w = v["tower_w"] / 2.0
    glass_plane = root.constructionPlanes.itemByName(PLANE_GLASS)

    sides = []
    for tag, tower_x, clamp_x in (("l", v["tower_x_l"], v["clamp_x_l"]),
                                 ("r", v["tower_x_r"], v["clamp_x_r"])):
        sign = 1.0 if clamp_x > tower_x else -1.0
        tower = (tower_x - half_w, tower_x + half_w)
        arm = tuple(sorted((tower_x - sign * half_w, clamp_x + sign * v["arm_end_over"])))
        sides.append((tag, tower, arm, clamp_x))

    sk = _fresh_sketch(root, BODY_PREFIX + "towers", glass_plane)
    sk.isComputeDeferred = True
    for _t, tower, _a, _cx in sides:
        _xy_rect(sk, {"x": tower, "y": plate_y})
    sk.isComputeDeferred = False
    _extrude(root, sk, FO.JoinFeatureOperation, arm_top - v["glass_z"], BODY_PREFIX + "towers_ext")

    sk = _fresh_sketch(root, BODY_PREFIX + "arms", _offset_plane(root, PLANE_HOLDER + "arm", arm_bot))
    for _t, _tw, arm, _cx in sides:
        _xy_rect(sk, {"x": arm, "y": plate_y})
    _extrude(root, sk, FO.JoinFeatureOperation, v["arm_thk"], BODY_PREFIX + "arms_ext")

    # bolt clearance straight through, then the insert seat bored from the top
    sk = _fresh_sketch(root, BODY_PREFIX + "bolts",
                       _offset_plane(root, PLANE_HOLDER + "bolt", arm_bot))
    for _t, _tw, _a, cx in sides:
        sk.sketchCurves.sketchCircles.addByCenterRadius(
            _pt(cx, clamp_y), (v["m3_shank_d"] / 2.0 + 0.2) * CM_PER_MM)
    _extrude(root, sk, FO.CutFeatureOperation, v["arm_thk"] + 1.0, BODY_PREFIX + "bolts_cut")

    sk = _fresh_sketch(root, BODY_PREFIX + "seats",
                       _offset_plane(root, PLANE_HOLDER + "seat", arm_top - v["insert_len"]))
    for _t, _tw, _a, cx in sides:
        sk.sketchCurves.sketchCircles.addByCenterRadius(
            _pt(cx, clamp_y), (v["insert_pilot_d"] / 2.0) * CM_PER_MM)
    _extrude(root, sk, FO.CutFeatureOperation, v["insert_len"] + 0.1, BODY_PREFIX + "seats_cut")

    # Cartridges: one body each. The compliant pad is left standing 0.2 mm below
    # the rigid rim by cutting the ring around it, so the rim bottoms on the card
    # and bounds the squeeze. Loose and swappable, so no socket is needed.
    #
    # Note: a Join extrude unions DISJOINT bodies too, so building the pad and
    # the rim as two bodies and joining them silently welds the cartridge into
    # BRK_bottom. One body per cartridge, or Fusion decides the target for us.
    for tag, _tw, _a, cx in sides:
        sk = _fresh_sketch(root, BODY_PREFIX + "cart_" + tag,
                           _offset_plane(root, PLANE_HOLDER + "cart" + tag, v["card_face_z"]))
        sk.sketchCurves.sketchCircles.addByCenterRadius(
            _pt(cx, clamp_y), (v["cartridge_od"] / 2.0) * CM_PER_MM)
        feat = _extrude(root, sk, FO.NewBodyFeatureOperation, v["cartridge_h"],
                        BODY_PREFIX + "cart" + tag + "_body")
        feat.bodies.item(0).name = BODY_PREFIX + "cart_" + tag

        sk = _fresh_sketch(root, BODY_PREFIX + "crelief_" + tag,
                           _offset_plane(root, PLANE_HOLDER + "crelief" + tag, v["card_face_z"]))
        circles = sk.sketchCurves.sketchCircles
        circles.addByCenterRadius(_pt(cx, clamp_y), (v["pad_od"] / 2.0) * CM_PER_MM)
        circles.addByCenterRadius(_pt(cx, clamp_y), (v["cartridge_od"] / 2.0 + 1.0) * CM_PER_MM)
        _extrude(root, sk, FO.CutFeatureOperation, v["pad_protrusion"],
                 BODY_PREFIX + "crelief" + tag + "_cut", largest_only=True)

        # centring dimple for the (bought) bolt tip
        _centring_dimple(root, "cart_" + tag, cx, clamp_y, v, cart_top)

    # Printed thumb knobs. A hex pocket grips the bolt head so the knob drives
    # it; the knob is its own printed part and simply lifts off.
    for tag, _tw, _a, cx in sides:
        sk = _fresh_sketch(root, BODY_PREFIX + "knob_" + tag,
                           _offset_plane(root, PLANE_HOLDER + "knob" + tag, arm_top))
        sk.sketchCurves.sketchCircles.addByCenterRadius(
            _pt(cx, clamp_y), (v["knob_od"] / 2.0) * CM_PER_MM)
        feat = _extrude(root, sk, FO.NewBodyFeatureOperation, v["knob_h"],
                        BODY_PREFIX + "knob" + tag + "_body")
        feat.bodies.item(0).name = BODY_PREFIX + "knob_" + tag

        sk = _fresh_sketch(root, BODY_PREFIX + "kpocket_" + tag,
                           _offset_plane(root, PLANE_HOLDER + "kpocket" + tag, arm_top))
        _hex(sk, cx, clamp_y, v["knob_pocket_af"])
        _extrude(root, sk, FO.CutFeatureOperation, v["knob_pocket_depth"],
                 BODY_PREFIX + "kpocket" + tag + "_cut")

    # Rail cartridge variant (A/B against the point pad). Same compliant-pad
    # idea, but a line contact instead of a point, so the card resists rotating
    # about a clamp axis. Printed as extra bodies; pick one per side.
    for tag, _tw, _a, cx in sides:
        half_l = v["rail_body_len"] / 2.0
        half_w = v["rail_body_w"] / 2.0
        sk = _fresh_sketch(root, BODY_PREFIX + "rail_" + tag,
                           _offset_plane(root, PLANE_HOLDER + "rail" + tag, v["card_face_z"]))
        _xy_rect(sk, {"x": (cx - half_l, cx + half_l), "y": (clamp_y - half_w, clamp_y + half_w)})
        feat = _extrude(root, sk, FO.NewBodyFeatureOperation, v["cartridge_h"],
                        BODY_PREFIX + "rail" + tag + "_body")
        feat.bodies.item(0).name = BODY_PREFIX + "rail_" + tag

        # ring cut between two nested rectangles leaves the rail pad proud
        phl, phw = v["rail_len"] / 2.0, v["rail_w"] / 2.0
        sk = _fresh_sketch(root, BODY_PREFIX + "railrelief_" + tag,
                           _offset_plane(root, PLANE_HOLDER + "railrelief" + tag, v["card_face_z"]))
        _xy_rect(sk, {"x": (cx - half_l - 1.0, cx + half_l + 1.0),
                      "y": (clamp_y - half_w - 1.0, clamp_y + half_w + 1.0)})
        _xy_rect(sk, {"x": (cx - phl, cx + phl), "y": (clamp_y - phw, clamp_y + phw)})
        _extrude(root, sk, FO.CutFeatureOperation, v["pad_protrusion"],
                 BODY_PREFIX + "railrelief" + tag + "_cut", largest_only=True)

        _centring_dimple(root, "rail_" + tag, cx, clamp_y, v, cart_top)

    return {"body": body.name, "boss_od": boss_od, "boss_h": boss_h,
            "half_x": half_x, "bar_y": plate_y, "arm_bot": arm_bot,
            "arm_top": arm_top, "cart_top": cart_top,
            "cartridges": [BODY_PREFIX + "cart_" + t for t, _tw, _a, _cx in sides]}


def build_top_bar(root, g: dict, v: dict) -> dict:
    """Top bar: bosses at H2/H3, a raised bridge, and an edge hook.

    The bridge rides *above* the card plane rather than resting on it, because
    START1 and SELECT1 stand 5.0 mm proud -- that is why the top cannot use a
    back plate the way the bottom does. The hook is the top's registration: the
    card overhangs the board by ~1.75 mm up here, so there is nothing behind it
    to clamp against.
    """
    from adsk.fusion import FeatureOperations as FO

    hole = {h["ref"]: h for h in g["holes"]}
    half_x = v["board_w"] / 2.0 - v["bar_inset"]
    boss_od = v["insert_od"] + 2.0 * v["boss_wall"]
    bridge_bot = v["card_face_z"] + v["bridge_clear"]
    bridge_top = bridge_bot + v["bridge_thk"]
    hook_y0 = v["card_top_y"] + v["hook_clear"]
    hook_y1 = hook_y0 + v["hook_thk"]

    sk = _fresh_sketch(root, BODY_PREFIX + "tbosses", root.xYConstructionPlane)
    circles = sk.sketchCurves.sketchCircles
    for ref in ("H2", "H3"):
        circles.addByCenterRadius(_pt(*hole[ref]["at"]), (boss_od / 2.0) * CM_PER_MM)
    feat = _extrude(root, sk, FO.NewBodyFeatureOperation, bridge_bot, BODY_PREFIX + "tbosses_ext")
    feat.bodies.item(0).name = BODY_PREFIX + "top"

    sk = _fresh_sketch(root, BODY_PREFIX + "bridge",
                       _offset_plane(root, PLANE_HOLDER + "bridge", bridge_bot))
    _xy_rect(sk, {"x": (-half_x, half_x), "y": (v["bridge_y0"], hook_y1)})
    _extrude(root, sk, FO.JoinFeatureOperation, v["bridge_thk"], BODY_PREFIX + "bridge_ext")

    # the hook hangs below the bridge, out past the card's top edge
    sk = _fresh_sketch(root, BODY_PREFIX + "hook",
                       _offset_plane(root, PLANE_HOLDER + "hook", v["hook_z"]))
    _xy_rect(sk, {"x": (v["hook_x_l"], v["hook_x_r"]), "y": (hook_y0, hook_y1)})
    _extrude(root, sk, FO.JoinFeatureOperation, bridge_top - v["hook_z"], BODY_PREFIX + "hook_ext")

    # blind insert seats, bored up from the board face; 0.8 mm of material left
    # above them so a bolt tip cannot emerge at the bridge's underside
    sk = _fresh_sketch(root, BODY_PREFIX + "tpilots", root.xYConstructionPlane)
    circles = sk.sketchCurves.sketchCircles
    for ref in ("H2", "H3"):
        circles.addByCenterRadius(_pt(*hole[ref]["at"]), (v["insert_pilot_d"] / 2.0) * CM_PER_MM)
    _extrude(root, sk, FO.CutFeatureOperation, v["insert_len"], BODY_PREFIX + "tpilots_cut")

    return {"body": BODY_PREFIX + "top", "boss_od": boss_od, "bridge_bot": bridge_bot,
            "bridge_top": bridge_top, "hook_y": (hook_y0, hook_y1)}


def build_coupon(root, g: dict, v: dict) -> dict:
    """A fit-check coupon, emitted from the same source as the holder.

    It settles the two numbers the plan says to dial in on hardware before
    committing the real parts: the PETG insert pilot diameter, and whether the
    board holes need a light ream for the M3 shank. It is a separate body, so
    the slicer picks it independently of the holder.
    """
    from adsk.fusion import FeatureOperations as FO

    ox, oy = v["coupon_x"], v["coupon_y"]
    plate_w, plate_d, plate_t = 100.0, 30.0, 3.0
    boss_od = v["insert_od"] + 2.0 * v["boss_wall"]
    boss_h = v["insert_len"]            # match the real boss depth
    n, span = 6, 15.0

    sk = _fresh_sketch(root, BODY_PREFIX + "coupon", root.xYConstructionPlane)
    _xy_rect(sk, {"x": (ox - plate_w / 2, ox + plate_w / 2),
                  "y": (oy - plate_d / 2, oy + plate_d / 2)})
    feat = _extrude(root, sk, FO.NewBodyFeatureOperation, plate_t, BODY_PREFIX + "coupon_ext")
    feat.bodies.item(0).name = BODY_PREFIX + "coupon"

    xs = [ox - span * (n - 1) / 2.0 + i * span for i in range(n)]

    sk = _fresh_sketch(root, BODY_PREFIX + "coupon_bosses",
                       _offset_plane(root, PLANE_HOLDER + "coupon", plate_t))
    circles = sk.sketchCurves.sketchCircles
    for x in xs:
        circles.addByCenterRadius(_pt(x, oy), (boss_od / 2.0) * CM_PER_MM)
    _extrude(root, sk, FO.JoinFeatureOperation, boss_h, BODY_PREFIX + "coupon_bosses_ext")

    pilots = [v["coupon_pilot_min"] + i * v["coupon_pilot_step"] for i in range(n)]
    sk = _fresh_sketch(root, BODY_PREFIX + "coupon_pilots", root.xYConstructionPlane)
    circles = sk.sketchCurves.sketchCircles
    for x, d in zip(xs, pilots):
        circles.addByCenterRadius(_pt(x, oy), (d / 2.0) * CM_PER_MM)
    _extrude(root, sk, FO.CutFeatureOperation, plate_t + boss_h + 1.0,
             BODY_PREFIX + "coupon_pilots_cut")

    return {"body": BODY_PREFIX + "coupon", "pilots": [round(d, 2) for d in pilots]}


def export_snapshots(design, root, repo) -> list:
    """Write derived artifacts to cad/export/.

    STEP is the reviewed artifact: ASCII, diffable, and usable without Fusion or
    an Autodesk account. Since Fusion has no local documents, this is the only
    way geometry leaves the cloud. STL is a slicing product; the .f3d is an
    archive snapshot. All of it is regenerated -- none of it is a source of truth.
    """
    import adsk.fusion

    tag = str(load_target(repo).get("name") or "card-holder")
    out = repo / "cad" / "export"
    out.mkdir(parents=True, exist_ok=True)

    em = design.exportManager
    written = []

    if root.bRepBodies.count == 0:
        print("exports: skipped STEP/STL -- no solid bodies yet "
              "(Fusion's STEP carries solids, not sketches)")
    else:
        step = out / f"{tag}.step"
        em.execute(em.createSTEPExportOptions(str(step)))
        written.append(step)

        for i in range(root.bRepBodies.count):
            body = root.bRepBodies.item(i)
            stl = out / f"{body.name}.stl"
            opts = em.createSTLExportOptions(body, str(stl))
            opts.meshRefinement = adsk.fusion.MeshRefinementSettings.MeshRefinementHigh
            opts.unitType = adsk.fusion.DistanceUnits.MillimeterDistanceUnits
            em.execute(opts)
            written.append(stl)

    f3d = out / f"{tag}.f3d"
    em.execute(em.createFusionArchiveExportOptions(str(f3d)))
    written.append(f3d)

    print("exports:")
    for p in written:
        size = p.stat().st_size if p.exists() else -1
        print(f"  {p.name:32s} {size:9d} bytes")
    return written


def run(_ctx):
    """Fusion entry point."""
    import adsk.core
    import adsk.fusion

    app = adsk.core.Application.get()
    design = adsk.fusion.Design.cast(app.activeProduct)
    if design is None:
        print("ERROR: no active design. Open a design (and save it to your hub) first.")
        return

    units = design.unitsManager
    if units.defaultLengthUnits.lower() != "mm":
        print(f"WARNING: document units are {units.defaultLengthUnits}; expected mm")

    root = design.rootComponent
    repo = find_repo()

    target = load_target(repo)
    active = app.activeDocument
    active_name = (active.name if active else "") or ""
    want = str(target.get("name") or "").strip()
    if want:
        got = active_name.split(".")[0].strip()
        if got.lower() != want.lower():
            print(f"ERROR: the active document is {active_name!r}, but "
                  f"cad/fusion/target.json says {want!r}.")
            print("Refusing to build into the wrong document. Open the right one, "
                  "or update target.json.")
            return
        print(f"target document: {got!r}")

    params_doc, ref = load_inputs(repo)
    defs = param_defs(params_doc)

    added, updated, total = sync_parameters(design, defs)
    print(f"parameters: +{added} new, {updated} updated, {total} total")

    names = [d["name"] for d in defs]
    vals = _read_params(design, names)
    g = geom(ref, vals)

    made, removed = build_reference(root, g)
    if removed:
        print(f"cleared {removed} previous reference sketch(es)")
    print("reference sketches: " + ", ".join(made))
    print()
    print(report(g, vals))
    print()

    cleared = _clear_holder(root)
    if cleared:
        print(f"cleared {cleared} previous holder item(s)")
    holder = build_holder(root, g, vals)
    top = build_top_bar(root, g, vals)
    coupon = build_coupon(root, g, vals)
    body = root.bRepBodies.itemByName(holder["body"])
    bb = body.boundingBox
    print(f"bottom bracket: {holder['body']}")
    print(f"  bosses D{holder['boss_od']:.2f} x {holder['boss_h']:.2f} tall, "
          f"plate {vals['backplate_thk']:.2f} thick, top at z={vals['glass_z']:.2f}")
    print(f"  bbox X {bb.minPoint.x * MM_PER_CM:+.3f} .. {bb.maxPoint.x * MM_PER_CM:+.3f}  "
          f"Y {bb.minPoint.y * MM_PER_CM:+.3f} .. {bb.maxPoint.y * MM_PER_CM:+.3f}  "
          f"Z {bb.minPoint.z * MM_PER_CM:+.3f} .. {bb.maxPoint.z * MM_PER_CM:+.3f}")
    print(f"  volume {body.volume:.3f} cm^3 ({body.volume * 1000:.0f} mm^3)")
    tip = holder["arm_top"] - vals["bolt_shank"]
    print(f"  clamp: cartridge top z={holder['cart_top']:.2f}, "
          f"arm {holder['arm_bot']:.2f}..{holder['arm_top']:.2f}, "
          f"bolt tip z={tip:.2f} (gap {tip - holder['cart_top']:+.2f})")
    print(f"  bodies ({root.bRepBodies.count}): "
          + ", ".join(root.bRepBodies.item(i).name for i in range(root.bRepBodies.count)))
    tb = root.bRepBodies.itemByName(top["body"])
    tbb = tb.boundingBox
    print(f"top bar: {top['body']}")
    print(f"  bridge z {top['bridge_bot']:.2f}..{top['bridge_top']:.2f}, "
          f"hook y {top['hook_y'][0]:.2f}..{top['hook_y'][1]:.2f} down to z={vals['hook_z']:.2f}")
    print(f"  bbox X {tbb.minPoint.x * MM_PER_CM:+.3f} .. {tbb.maxPoint.x * MM_PER_CM:+.3f}  "
          f"Y {tbb.minPoint.y * MM_PER_CM:+.3f} .. {tbb.maxPoint.y * MM_PER_CM:+.3f}  "
          f"Z {tbb.minPoint.z * MM_PER_CM:+.3f} .. {tbb.maxPoint.z * MM_PER_CM:+.3f}")
    print(f"  volume {tb.volume:.3f} cm^3 ({tb.volume * 1000:.0f} mm^3)")
    cb = root.bRepBodies.itemByName(coupon["body"])
    print(f"coupon: {coupon['body']} -- insert pilots "
          + ", ".join(f"D{d}" for d in coupon["pilots"]))
    print(f"  volume {cb.volume:.3f} cm^3 ({cb.volume * 1000:.0f} mm^3)")
    print()
    bad = checks(g, vals)
    if bad:
        print(f"WARNING: {len(bad)} clearance check(s) failed:")
        for b in bad:
            print(f"  - {b}")
    else:
        print("clearance checks: all pass")
    print()
    export_snapshots(design, root, repo)


def main() -> int:
    repo = find_repo()
    params_doc, ref = load_inputs(repo)
    defs = param_defs(params_doc)
    vals = eval_params(defs)
    g = geom(ref, vals)
    print(f"repo: {repo}")
    print(f"parameters: {len(defs)} ({sum(1 for d in defs if 'PROVISIONAL' in d['comment'])} provisional)")
    print()
    print(report(g, vals))

    bad = checks(g, vals)
    if bad:
        print(f"\nFAIL: {len(bad)} clearance check(s):", file=sys.stderr)
        for b in bad:
            print(f"  - {b}", file=sys.stderr)
        return 1
    print("\nclearance checks: all pass")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
