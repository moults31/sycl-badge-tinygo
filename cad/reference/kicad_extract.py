#!/usr/bin/env python3
"""Extract the reference geometry the card holder is designed against.

Text in, text out. The source of truth is the KiCad board file in the hardware
repo; we read its s-expressions directly so this works with only Python 3 --
no KiCad, no kicad-cli, and no third-party packages.

This exists so that the board outline, the mounting-hole positions, and the
drill diameters are *derived* from the hardware rather than typed into a CAD
sketch by hand. Nothing here is measured, guessed, or drawn.

    python3 cad/reference/kicad_extract.py           # write board.json
    python3 cad/reference/kicad_extract.py --check   # also assert the plan's specs

The design of record is docs/pokemon-card-holder.md; `--check` verifies the
extracted geometry against the published board specs there, so a board revision
or a typo in the doc fails loudly instead of silently.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
OUT_PATH = REPO_ROOT / "cad" / "reference" / "board.json"

# The hardware repo is a separate checkout (see docs/pokemon-card-holder.md).
# Override with --board or $SYCL_BOARD_PCB.
CANDIDATES = [
    Path(os.environ["SYCL_BOARD_PCB"]) if os.environ.get("SYCL_BOARD_PCB") else None,
    Path.home() / "code" / "sycl_land" / "sycl-badge" / "kicad" / "v2" / "SYCL Badge 2024.kicad_pcb",
    REPO_ROOT.parent / "sycl-badge" / "kicad" / "v2" / "SYCL Badge 2024.kicad_pcb",
    REPO_ROOT.parent.parent / "sycl-badge" / "kicad" / "v2" / "SYCL Badge 2024.kicad_pcb",
]

# The published board specs (docs/pokemon-card-holder.md, Appendix B).
#
# These are the *nominal* numbers the plan publishes. The board file is the
# source of truth, and it does not reproduce them exactly: the drawn outline is
# a hand-authored polyline + arcs, so its corners are R3.00-R3.12 rather than a
# clean R3.0, and the four holes form a 101.88 x 55.91-55.93 rectangle rather
# than a perfect one. Two separate tolerances keep that honest:
#
#   TOL_IDENTITY     - "is this the board we think it is?" Catches a wrong file
#                      or a board revision. Applied to hole positions (the
#                      mating datum, so it must be exact) and the bbox.
#   TOL_IDEALIZATION - how far the real geometry may sit from the plan's
#                      rounded numbers before we flag it for the reader.
#
# Deviations inside TOL_IDENTITY but outside TOL_IDEALIZATION are printed as
# notes, never silently rounded away. The model is built from the extracted
# values, not from the nominal ones.
EXPECTED = {
    "_comment": "docs/pokemon-card-holder.md > Appendix B - Measured values > Board",
    "bbox": {"x": (96.406, 206.414), "y": (72.100, 136.106)},
    "size": (110.008, 64.006),
    "corner_radius": 3.0,
    "hole_bore": 3.0,
    "holes": {
        "H2": (100.455, 76.170),
        "H3": (202.335, 76.150),
        "H4": (100.445, 132.080),
        "H5": (202.305, 132.050),
    },
}
TOL_IDENTITY = 0.05      # mm - wrong board / bad parse
TOL_IDEALIZATION = 0.02  # mm - plan rounds here


# ---------------------------------------------------------------- s-expression

def tokenize(text: str):
    """Yield '(' , ')', or ('atom'|'str', value) from a KiCad s-expression file."""
    i, n = 0, len(text)
    while i < n:
        c = text[i]
        if c in " \t\r\n":
            i += 1
        elif c in "()":
            yield c
            i += 1
        elif c == '"':
            j, buf = i + 1, []
            while j < n:
                if text[j] == "\\" and j + 1 < n:
                    buf.append(text[j + 1])
                    j += 2
                    continue
                if text[j] == '"':
                    break
                buf.append(text[j])
                j += 1
            yield ("str", "".join(buf))
            i = j + 1
        else:
            j = i
            while j < n and text[j] not in ' \t\r\n()"':
                j += 1
            yield ("atom", text[i:j])
            i = j


def parse(text: str):
    """Parse the whole file into nested lists; return the single top-level node."""
    stack: list[list] = [[]]
    for tok in tokenize(text):
        if tok == "(":
            stack.append([])
        elif tok == ")":
            node = stack.pop()
            if not stack:
                raise ValueError("unbalanced parentheses in source")
            stack[-1].append(node)
        else:
            stack[-1].append(tok[1])
    if len(stack) != 1 or len(stack[0]) != 1:
        raise ValueError(f"expected exactly one top-level node, got {len(stack[0])}")
    return stack[0][0]


def kids(node, tag: str):
    return [c for c in node[1:] if isinstance(c, list) and c and c[0] == tag]


def kid(node, tag: str):
    found = kids(node, tag)
    return found[0] if found else None


def f(x) -> float:
    return float(x)


def xy(node) -> list[float]:
    return [f(node[1]), f(node[2])]


def circumradius(a, b, c) -> float:
    """Radius of the circle through three (x, y) points."""
    (ax, ay), (bx, by), (cx, cy) = a, b, c
    d = 2.0 * (ax * (by - cy) + bx * (cy - ay) + cx * (ay - by))
    if abs(d) < 1e-12:
        return float("inf")
    ux = ((ax**2 + ay**2) * (by - cy) + (bx**2 + by**2) * (cy - ay)
          + (cx**2 + cy**2) * (ay - by)) / d
    uy = ((ax**2 + ay**2) * (cx - bx) + (bx**2 + by**2) * (ax - cx)
          + (cx**2 + cy**2) * (bx - ax)) / d
    return math.hypot(ax - ux, ay - uy)


def _world_aabb(at, w, h, rot_deg):
    """World-space AABB of a footprint's local bounding box, rotation applied.

    A circle through the local bbox is a fine keep-out for a square-ish part but
    wildly pessimistic for a long thin one (the AAA holder is ~63 mm end to end,
    so its circle claims 32 mm of radius everywhere). The rotated rectangle is
    the better primitive for deciding whether a bolt head has room.

    Sign convention: local (x, y) -> world (x0 + x cos t + y sin t,
    y0 - x sin t + y cos t). Only the AABB is needed, and for the square-ish
    parts that dominate here the two conventions agree.
    """
    t = math.radians(rot_deg)
    c, s = math.cos(t), math.sin(t)
    x0, y0 = at
    xs, ys = [], []
    for dx in (-w / 2.0, w / 2.0):
        for dy in (-h / 2.0, h / 2.0):
            xs.append(x0 + dx * c + dy * s)
            ys.append(y0 - dx * s + dy * c)
    return [min(xs), min(ys), max(xs), max(ys)]


def footprint_points(fp) -> list[tuple[float, float]]:
    """Every graphic point of a footprint, in footprint-local coordinates."""
    pts: list[tuple[float, float]] = []
    for tag in ("fp_line", "fp_rect", "fp_arc", "fp_circle", "fp_poly"):
        for g in kids(fp, tag):
            for t in ("start", "end", "mid", "center"):
                n = kid(g, t)
                if n:
                    pts.append((f(n[1]), f(n[2])))
            poly = kid(g, "pts")
            if poly:
                pts.extend((f(xy[1]), f(xy[2])) for xy in kids(poly, "xy"))
    return pts


def side_footprints(pcb, layer_name: str) -> list[dict]:
    """Parts on one board side, as conservative keep-outs.

    Rotation is deliberately ignored and each part is reported as a circle that
    contains its whole local bounding box. That over-estimates rather than
    under-estimates, which is the safe direction for a collision check: a part
    it says is clear really is clear. The plan only ever tabulated the six front
    controls, so everything else was invisible until this existed.

    The back side matters too: the mounting bolts pass through from the back, so
    their heads and spacers need clear space there.
    """
    out = []
    for fp in kids(pcb, "footprint"):
        layer = kid(fp, "layer")
        if not layer or layer[1] != layer_name:
            continue
        at = kid(fp, "at")
        pts = footprint_points(fp)
        if not pts:
            continue
        xs = [p[0] for p in pts]
        ys = [p[1] for p in pts]
        w, h = max(xs) - min(xs), max(ys) - min(ys)
        out.append({
            "ref": next((p[2] for p in kids(fp, "property") if p[1] == "Reference"), None),
            "value": next((p[2] for p in kids(fp, "property") if p[1] == "Value"), None),
            "footprint": fp[1],
            "at": [f(at[1]), f(at[2])],
            "rot_deg": f(at[3]) if len(at) > 3 else 0.0,
            "local_size": [round(w, 3), round(h, 3)],
            "conservative_radius": round(math.hypot(w, h) / 2.0, 3),
            "keepout_rect": [round(x, 3) for x in _world_aabb([f(at[1]), f(at[2])], w, h,
                                                             f(at[3]) if len(at) > 3 else 0.0)],
            "pads": len(kids(fp, "pad")),
            "graphic_layers": sorted(
                {kid(g, "layer")[1] for tag in ("fp_line", "fp_rect", "fp_arc", "fp_circle", "fp_poly")
                 for g in kids(fp, tag) if kid(g, "layer")}
            ),
        })
    return out


# -------------------------------------------------------------------- extract

def extract(pcb) -> dict:
    outline = []
    for tag, kind in (("gr_line", "line"), ("gr_arc", "arc")):
        for node in kids(pcb, tag):
            layer = kid(node, "layer")
            if not layer or layer[1] != "Edge.Cuts":
                continue
            seg = {"kind": kind, "start": xy(kid(node, "start")), "end": xy(kid(node, "end"))}
            if kind == "arc":
                seg["mid"] = xy(kid(node, "mid"))
            outline.append(seg)
    if not outline:
        raise ValueError('no Edge.Cuts geometry found -- is this the right board file?')

    holes = []
    for fp in kids(pcb, "footprint"):
        if not isinstance(fp[1], str) or not fp[1].startswith("MountingHole"):
            continue
        ref = next((p[2] for p in kids(fp, "property") if p[1] == "Reference"), None)
        drill = next((f(kid(p, "drill")[1]) for p in kids(fp, "pad") if kid(p, "drill")), None)
        holes.append({"ref": ref, "at": xy(kid(fp, "at")), "drill": drill})

    pts = [p for seg in outline for p in (seg["start"], seg["end"], *([] if "mid" not in seg else [seg["mid"]]))]
    xs = [p[0] for p in pts]
    ys = [p[1] for p in pts]

    radii = [circumradius(s["start"], s["mid"], s["end"]) for s in outline if s["kind"] == "arc"]
    radii = [r for r in radii if math.isfinite(r)]
    if holes:
        hx = [h["at"][0] for h in holes]
        hy = [h["at"][1] for h in holes]
        grid = [round(max(hx) - min(hx), 3), round(max(hy) - min(hy), 3)]
    else:
        grid = None

    return {
        "format": "sycl-card-holder-reference/1",
        "generated_by": "cad/reference/kicad_extract.py",
        "units": "mm",
        "note": "Edge.Cuts geometry, verbatim. KiCad PCB coordinates: y increases downward.",
        "outline": outline,
        "bbox": {"x": [min(xs), max(xs)], "y": [min(ys), max(ys)],
                 "w": max(xs) - min(xs), "h": max(ys) - min(ys)},
        "mounting_holes": holes,
        "front_footprints": side_footprints(pcb, "F.Cu"),
        "back_footprints": side_footprints(pcb, "B.Cu"),
        "derived": {"corner_radii": [round(r, 4) for r in radii],
                    "hole_grid": grid},
    }


def check(doc: dict) -> tuple[list[str], list[str]]:
    """Gate identity against the plan; report (never hide) nominal deviations.

    Returns (failures, notes). Failures mean the file is not the board the plan
    describes. Notes mean the file *is* the board, but the plan's numbers are
    rounded ideals, listed so nothing is silently rounded away.
    """
    bad: list[str] = []
    notes: list[str] = []

    def close(label, got, want, tol):
        if abs(got - want) > tol:
            bad.append(f"{label}: got {got:.4f}, plan says {want:.4f} (tol {tol})")

    bb = doc["bbox"]
    close("bbox x min", bb["x"][0], EXPECTED["bbox"]["x"][0], TOL_IDENTITY)
    close("bbox x max", bb["x"][1], EXPECTED["bbox"]["x"][1], TOL_IDENTITY)
    close("bbox y min", bb["y"][0], EXPECTED["bbox"]["y"][0], TOL_IDENTITY)
    close("bbox y max", bb["y"][1], EXPECTED["bbox"]["y"][1], TOL_IDENTITY)

    got = {h["ref"]: h for h in doc["mounting_holes"]}
    if set(got) != set(EXPECTED["holes"]):
        bad.append(f"hole refs: got {sorted(got)}, plan says {sorted(EXPECTED['holes'])}")
    for ref, (ex, ey) in EXPECTED["holes"].items():
        if ref not in got:
            continue
        close(f"{ref} x", got[ref]["at"][0], ex, TOL_IDENTITY)
        close(f"{ref} y", got[ref]["at"][1], ey, TOL_IDENTITY)
        close(f"{ref} drill", got[ref]["drill"] or 0.0, EXPECTED["hole_bore"], TOL_IDENTITY)

    # --- nominal deviations (informational) ---------------------------------
    dw = bb["w"] - EXPECTED["size"][0]
    dh = bb["h"] - EXPECTED["size"][1]
    if abs(dw) > TOL_IDEALIZATION or abs(dh) > TOL_IDEALIZATION:
        notes.append(f"outline is {bb['w']:.3f} x {bb['h']:.3f} mm; "
                     f"plan rounds to {EXPECTED['size'][0]} x {EXPECTED['size'][1]} "
                     f"(delta {dw:+.3f} / {dh:+.3f})")

    for i, r in enumerate(doc["derived"]["corner_radii"]):
        if abs(r - EXPECTED["corner_radius"]) > TOL_IDEALIZATION:
            notes.append(f"corner arc {i} is R{r:.4f}; plan rounds to "
                         f"R{EXPECTED['corner_radius']} (delta {r - EXPECTED['corner_radius']:+.4f}). "
                         f"The drawn arc is preserved, not idealised.")

    holes = doc["mounting_holes"]
    if len(holes) == 4:
        by = {h["ref"]: h for h in holes}
        pairs = [("H2", "H3", "top x"), ("H4", "H5", "bottom x"),
                 ("H2", "H4", "left y"), ("H3", "H5", "right y")]
        spans = []
        for a, b, label in pairs:
            if a in by and b in by:
                da = abs(by[b]["at"][0] - by[a]["at"][0])
                db = abs(by[b]["at"][1] - by[a]["at"][1])
                spans.append(f"{label} {max(da, db):.3f}")
        notes.append("hole spans (not a perfect rectangle): " + ", ".join(spans))
    return bad, notes


# ----------------------------------------------------------------------- main

def resolve_source(explicit: str | None) -> Path:
    if explicit:
        p = Path(explicit).expanduser()
        if not p.is_file():
            sys.exit(f"error: --board {p} does not exist")
        return p
    for c in CANDIDATES:
        if c and c.is_file():
            return c
    sys.exit(
        "error: could not find 'SYCL Badge 2024.kicad_pcb'.\n"
        "  Set $SYCL_BOARD_PCB or pass --board <path>.\n"
        "  Checked:\n    " + "\n    ".join(str(c) for c in CANDIDATES if c)
    )


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--board", help="path to SYCL Badge 2024.kicad_pcb")
    ap.add_argument("-o", "--out", default=str(OUT_PATH), help="output JSON path")
    ap.add_argument("--check", action="store_true", help="assert the extracted geometry against the plan")
    ap.add_argument("--stdout", action="store_true", help="print instead of writing a file")
    args = ap.parse_args()

    src = resolve_source(args.board)
    raw = src.read_bytes()
    doc = extract(parse(raw.decode("utf-8")))

    # Provenance: what exactly this was derived from, so drift is detectable.
    try:
        rel = os.path.relpath(src, REPO_ROOT)
    except ValueError:
        rel = str(src)
    doc["source"] = {"path": rel, "sha256": hashlib.sha256(raw).hexdigest(), "bytes": len(raw)}

    text = json.dumps(doc, indent=2) + "\n"
    if args.stdout:
        print(text, end="")
    else:
        out = Path(args.out)
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(text)
        print(f"wrote {out.relative_to(REPO_ROOT)}  "
              f"({len(doc['outline'])} outline segments, {len(doc['mounting_holes'])} holes, "
              f"{len(doc['front_footprints'])} front + {len(doc['back_footprints'])} back parts, "
              f"board {doc['bbox']['w']:.3f} x {doc['bbox']['h']:.3f} mm)")

    if args.check:
        failures, notes = check(doc)
        for n in notes:
            print(f"note: {n}")
        if failures:
            print(f"\nFAIL: {len(failures)} mismatch(es) against the plan:", file=sys.stderr)
            for b in failures:
                print(f"  - {b}", file=sys.stderr)
            return 1
        print("check: identity OK -- this is the board the plan describes")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
