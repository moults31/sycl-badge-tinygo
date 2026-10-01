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

    return {
        "origin_kicad": (cx, cy),
        "outline": outline,
        "holes": holes,
        "board": board,
        "sleeve": sleeve,
        "card": card,
        "art": art,
        "active": active,
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

    _clear_holder(root)

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

    return {"body": body.name, "boss_od": boss_od, "boss_h": boss_h,
            "half_x": half_x, "bar_y": plate_y, "arm_bot": arm_bot,
            "arm_top": arm_top, "cart_top": cart_top,
            "cartridges": [BODY_PREFIX + "cart_" + t for t, _tw, _a, _cx in sides]}


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

    holder = build_holder(root, g, vals)
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
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
