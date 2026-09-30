#!/usr/bin/env python3
"""Turn card art-box images into a baked lightshow asset for the badge.

The badge backlights a physical card laid on the LCD. Light diffuses through
the card stock, so the asset is a *coarse, soft luminance mask* (the "glow"
map) plus a small palette derived from the art -- not the art itself. Fine
detail would blur away, so the mask is tiny (a few hundred bytes) and gets
bilinearly upscaled on-device.

Inputs
------
- One or more card images supplied by the user (PNG/JPG/WebP), cropped to the
  art box. We do NOT commit third-party card imagery -- keep it under
  ``assets/cards/`` (gitignored). Bring your own.
- Or ``--sample``: an original, generated test creature, so the pipeline and
  the simulator can be exercised with no copyrighted input at all.

Single card:   pass an image (or --sample) and the crop/colour flags.
Many cards:    pass --manifest cards.json, a list of entries like
               {"file": "x.webp", "name": "X", "art_box": [l,t,r,b],
                "lcd_window": [l,t,r,b], "glow_color": [r,g,b], ...}.
               Entries whose file is missing are skipped; if none resolve, the
               synthetic sample is emitted so the build still compiles.

Glow modes (per card)
---------------------
- Default: luminance of the art (backlights where the card is bright).
- ``subject: "auto"`` -- U2-Net saliency matting (the ``rembg`` package, CPU)
  extracts the subject's silhouette; the mask is that silhouette multiplied by
  the stretched luminance, so the creature glows out of a dark background and
  the card reads as figure on ground instead of a luminance photo. If rembg or
  its model is not installed, the build falls back to plain luminance with a
  warning (so CI and fresh clones still compile; install rembg on the build
  machine for the good look).
- ``subject: "luma" | "sat" | "luma*sat"`` — hand-free derivations. ``sat`` is
  the color-saturation (max-min/max) channel percentile-stretched; ``luma*sat``
  requires both. These separate subject from background when the *background*
  is the bright or the vivid part.
- ``invert``: luminance inverted (lights the card's dark line-art/features).
- ``mask``: paint your own glow/silhouette over the card image and use that --
  always wins, overriding every automatic mode.

Output
------
A Go source file (package ``cartridge``) defining ``var cards = []CardAsset{...}``
(the default), or, with ``--format json``, the same assets as JSON for the host
simulator (``cmd/simui``) to load at runtime. Each asset also carries an
``ambient`` RGB triple - the background wash tint used by the render to keep
the unlit card dim and separate from the subject. The generated Go file is
gitignored and rebuilt by ``make`` (see the Makefile).
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
from dataclasses import dataclass
from typing import Iterable

from PIL import Image, ImageDraw, ImageOps

# Rec.709 luma weights.
LUMA = (0.2126, 0.7152, 0.0722)

# Standard Pokemon card and badge LCD active-area dimensions, in millimetres.
CARD_W_MM = 63.5
LCD_W_MM = 35.04
LCD_H_MM = 28.03


@dataclass
class Asset:
    name: str
    set_: str
    types: list[str]
    rarity: str
    width: int
    height: int
    mask: bytes  # width*height 4-bit values, packed two per byte (high nibble first)
    palette: list[tuple[int, int, int]]  # dark -> bright
    ambient: tuple[int, int, int] = (0, 0, 0)  # background wash tint


def rgb565(r: int, g: int, b: int) -> int:
    return ((r & 0xF8) << 8) | ((g & 0xFC) << 3) | (b >> 3)


def luma(px: tuple[int, int, int]) -> float:
    r, g, b = px[:3]
    return LUMA[0] * r + LUMA[1] * g + LUMA[2] * b


# --- sample art -------------------------------------------------------------

def make_sample_art(w: int = 400, h: int = 560) -> Image.Image:
    """An original, abstract creature on a dark gradient.

    Deliberately generic: a bright subject on a dark field, so the luminance
    mask reads as a glowing silhouette. No resemblance to any real card.
    """
    img = Image.new("RGB", (w, h), (10, 14, 30))
    d = ImageDraw.Draw(img)

    # Vertical gradient background (dark navy -> teal).
    for y in range(h):
        t = y / (h - 1)
        d.line([(0, y), (w, y)], fill=(int(10 + 8 * t), int(14 + 40 * t), int(30 + 30 * t)))

    cx = w // 2
    body = (250, 180, 60)
    accent = (255, 240, 150)

    # Ears.
    d.polygon([(cx - 95, 250), (cx - 40, 130), (cx - 20, 265)], fill=body)
    d.polygon([(cx + 95, 250), (cx + 40, 130), (cx + 20, 265)], fill=body)

    # Head + body.
    d.ellipse([cx - 110, 200, cx + 110, 400], fill=body)
    d.ellipse([cx - 120, 330, cx + 120, 520], fill=body)

    # Cheeks + belly highlight.
    d.ellipse([cx - 95, 300, cx - 45, 345], fill=(255, 120, 90))
    d.ellipse([cx + 45, 300, cx + 95, 345], fill=(255, 120, 90))
    d.ellipse([cx - 55, 360, cx + 55, 500], fill=accent)

    # Eyes.
    d.ellipse([cx - 60, 270, cx - 20, 320], fill=(20, 20, 30))
    d.ellipse([cx + 20, 270, cx + 60, 320], fill=(20, 20, 30))
    d.ellipse([cx - 50, 280, cx - 38, 297], fill=(255, 255, 255))
    d.ellipse([cx + 30, 280, cx + 42, 297], fill=(255, 255, 255))

    return img


# --- geometry ---------------------------------------------------------------

def crop_art(img: Image.Image, box: Iterable[int] | None) -> Image.Image:
    if box is None:
        return img
    left, top, right, bottom = box
    if not (0 <= left < right <= img.width and 0 <= top < bottom <= img.height):
        raise SystemExit(
            f"art box {tuple(box)} is outside the {img.width}x{img.height} image"
        )
    return img.crop((left, top, right, bottom))


def crop_window(art: Image.Image, win: Iterable[float] | None) -> Image.Image:
    """Crop the art box to the region that actually sits over the LCD.

    ``win`` is normalized (0..1) coordinates within the art box. The panel is
    smaller than the art box, so the mask should cover exactly the footprint
    under the LCD -- otherwise the glow is a scaled-down picture of the whole
    art and won't line up with the print. The placement/calibration trim then
    absorbs any residual offset.
    """
    if win is None:
        return art
    l, t, r, b = win
    if not (0 <= l < r <= 1 and 0 <= t < b <= 1):
        raise SystemExit(f"lcd_window {tuple(win)} must be ordered fractions in 0..1")
    w, h = art.size
    box = (round(l * w), round(t * h), round(r * w), round(b * h))
    if box[0] >= box[2] or box[1] >= box[3]:
        raise SystemExit(f"lcd_window {tuple(win)} is empty after rounding")
    return art.crop(box)


# --- glow -------------------------------------------------------------------

def glow_gray(img: Image.Image, gamma: float, floor: float) -> Image.Image:
    """Luminance -> a soft 'glow' grayscale, with gamma and a black floor."""
    rgb = img.convert("RGB")
    lum = Image.new("L", rgb.size)
    lum.putdata([int(luma(p)) for p in rgb.getdata()])
    lut = []
    for v in range(256):
        a = (v / 255.0) ** gamma
        if floor > 0:
            a = max(0.0, (a - floor) / (1.0 - floor))
        lut.append(int(round(a * 255)))
    return lum.point(lut)


def sat_gray(img: Image.Image) -> Image.Image:
    """Color saturation (max-min)/max per pixel, as an L image."""
    rgb = img.convert("RGB")
    s = Image.new("L", rgb.size)
    s.putdata([
        0 if mx == 0 else int((mx - mn) * 255 / mx)
        for mx, mn in ((max(p), min(p)) for p in rgb.getdata())
    ])
    return s


def percentile_stretch(gray: Image.Image, lo_q: float = 0.02, hi_q: float = 0.98) -> Image.Image:
    """Percentile contrast stretch, applied after the coarse-grid resize.

    Card art tends to sit in a narrow luma band (p10..p90 can span <50 gray
    levels), which is what makes the luminance mask read as a flat wash.
    """
    vals = sorted(gray.getdata())
    n = len(vals)
    lo = vals[int(n * lo_q)]
    hi = vals[min(n - 1, int(n * hi_q))]
    if hi <= lo:
        return gray
    return gray.point([min(255, max(0, (v - lo) * 255 / (hi - lo))) for v in range(256)])


_REMBG_SESSION = None
# Names of cards whose `subject: auto` fell back to plain luminance, for the
# end-of-run warning.
_auto_fallbacks: list[str] = []


def subject_matte(art: Image.Image) -> Image.Image | None:
    """U2-Net saliency matte of the art's subject ('rembg', CPU), or None.

    Returns a grayscale L image, white where the salient subject is. Runs on
    the art at full art-box resolution (the model downsamples internally), so
    one call per card regardless of the mask grid.
    """
    global _REMBG_SESSION
    try:
        from rembg import remove, new_session
    except ImportError:
        return None
    try:
        if _REMBG_SESSION is None:
            _REMBG_SESSION = new_session("u2net")
        m = remove(art, session=_REMBG_SESSION, only_mask=True)
        return m if m.mode == "L" else m.convert("L")
    except Exception as e:  # model file unreadable, runtime failure, ...
        print(f"make_card: rembg failed ({e}); falling back to luminance", file=sys.stderr)
        return None


def load_glow(img: Image.Image, art_box, lcd_window, w: int, h: int,
              gamma: float, floor: float, invert: bool,
              subject: str | None = None, name: str = "") -> Image.Image:
    """Build the coarse glow map: luminance, or subject-mode separations."""
    art = crop_window(crop_art(img, art_box), lcd_window)
    if subject == "auto":
        matte = subject_matte(art)
        if matte is not None:
            lum = percentile_stretch(glow_gray(art, gamma, floor).resize((w, h), Image.BOX))
            sil = matte.resize((w, h), Image.BOX)
            out = Image.new("L", (w, h))
            # Silhouette-gated luminance: the subject keeps its stretched
            # luminance, the background keeps only a whisper of glow so the
            # figure/ground split survives the diffusion.
            out.putdata([(l * int(s) + 8 * (255 - int(s))) // 255
                         for l, s in zip(lum.getdata(), sil.getdata())])
            return out
        _auto_fallbacks.append(name or "card")
    if subject == "sat":
        return percentile_stretch(sat_gray(art).resize((w, h), Image.BOX))
    if subject == "luma*sat":
        lum = percentile_stretch(glow_gray(art, gamma, floor).resize((w, h), Image.BOX))
        sat = percentile_stretch(sat_gray(art).resize((w, h), Image.BOX))
        out = Image.new("L", (w, h))
        # Geometric-mean-ish combine: needs both channels to light up.
        out.putdata([min(255, l * s * 2 // (255 * 255) * 184) for l, s in zip(lum.getdata(), sat.getdata())])
        return out
    gray = glow_gray(art, gamma, floor).resize((w, h), Image.BOX)
    if invert:
        gray = ImageOps.invert(gray)
    return gray


def load_mask(path: str, art_box, lcd_window, w: int, h: int) -> Image.Image:
    """Use a user-painted glow/silhouette, registered to the card image.

    The mask is expected in the same pixel space as the source card image (paint
    over the card in any editor). Its alpha channel is used if present, so an
    RGBA PNG with the silhouette painted on a transparent background works
    directly; otherwise its grayscale is used.
    """
    m = Image.open(path)
    if m.mode in ("RGBA", "LA", "PA"):
        m = m.getchannel("A")
    else:
        m = m.convert("L")
    m = crop_window(crop_art(m, art_box), lcd_window)
    return m.resize((w, h), Image.BOX)


def quantize_4bit(gray: Image.Image) -> list[int]:
    return [min(15, max(0, round(v * 15 / 255))) for v in gray.getdata()]


def pack_4bit(vals: list[int]) -> bytes:
    out = bytearray()
    for i in range(0, len(vals), 2):
        hi = vals[i] & 0xF
        lo = vals[i + 1] & 0xF if i + 1 < len(vals) else 0
        out.append((hi << 4) | lo)
    return bytes(out)


# --- palette ----------------------------------------------------------------

def kmeans(samples: list[tuple[int, int, int]], k: int, iters: int = 12) -> list[tuple[int, int, int]]:
    """Tiny pure-Python k-means; sample counts here are ~1e3, so this is fine."""
    # Deterministic init: pick k points spaced through a luma-sorted list.
    ordered = sorted(samples, key=luma)
    if len(ordered) <= k:
        return ordered
    step = len(ordered) / k
    centroids = [ordered[min(len(ordered) - 1, int(i * step))] for i in range(k)]

    for _ in range(iters):
        acc = [[0.0, 0.0, 0.0, 0] for _ in range(k)]
        for s in samples:
            best, bestd = 0, float("inf")
            for i, c in enumerate(centroids):
                d = sum((s[j] - c[j]) ** 2 for j in range(3))
                if d < bestd:
                    best, bestd = i, d
            a = acc[best]
            for j in range(3):
                a[j] += s[j]
            a[3] += 1
        for i, a in enumerate(acc):
            if a[3]:
                centroids[i] = (int(a[0] / a[3]), int(a[1] / a[3]), int(a[2] / a[3]))
    return sorted(set(centroids), key=luma)


def derive_palette(img: Image.Image, coarse: Image.Image, k: int) -> list[tuple[int, int, int]]:
    """A 3-stop glow ramp: near-black -> the card's signature color -> white.

    Backlighting reads best as a single vivid tint that blows out to white where
    the art is brightest, rather than as a full color reproduction (light
    diffuses through the card stock anyway).
    """
    rgb = img.resize(coarse.size, Image.BOX).convert("RGB")
    samples = list(rgb.getdata())
    clusters = kmeans(samples, max(3, k))

    def sat(c: tuple[int, int, int]) -> float:
        mx, mn = max(c), min(c)
        return 0.0 if mx == 0 else (mx - mn) / mx

    bright = [c for c in clusters if luma(c) > 0.30 * 255] or clusters
    vivid = max(bright, key=lambda c: sat(c) * 1.0 + luma(c) / 255.0 * 0.5)

    dark = min(clusters, key=luma)
    dark = tuple(int(v * 0.10) for v in dark)  # unlit card reads as near-off
    # Ambient = where the unlit card settles: a very dim tint of the vivid
    # color, so the background wash reads as the card's own palette shadow.
    ambient = tuple(min(255, int(v * 0.22)) for v in vivid)
    return [dark, vivid, (255, 255, 255)], ambient  # type: ignore[return-value]


# --- asset ------------------------------------------------------------------

def fitted_window(img_w: int, art_box) -> list[float]:
    """A centered LCD-sized window within the art box, as normalized fractions.

    Uses the real card width (63.5 mm) to convert source pixels to millimetres,
    then centres the 35.04x28.03 mm panel footprint in the art box. This is the
    default until the physical holder is measured.
    """
    if art_box is None:
        raise SystemExit("lcd_window 'fit' needs an art box")
    l, t, r, b = art_box
    aw, ah = r - l, b - t
    pxmm = img_w / CARD_W_MM
    wpx, hpx = LCD_W_MM * pxmm, LCD_H_MM * pxmm
    x0, y0 = l + (aw - wpx) / 2, t + (ah - hpx) / 2
    return [(x0 - l) / aw, (y0 - t) / ah, (x0 - l + wpx) / aw, (y0 - t + hpx) / ah]


def build_asset(img: Image.Image, *, art_box, lcd_window, grid, gamma: float,
                floor: float, invert: bool, mask_path: str | None, palette: int,
                glow_color, subject: str | None, ambient: list[int] | None,
                name: str, set_: str, types: list[str], rarity: str) -> Asset:
    if lcd_window == "fit":
        lcd_window = fitted_window(img.width, art_box)
    art = crop_window(crop_art(img, art_box), lcd_window)
    w, h = grid
    if mask_path:
        coarse = load_mask(mask_path, art_box, lcd_window, w, h)
    else:
        # Default to the automatic subject matte: arbitrary cards get the
        # figure-on-ground look without per-card config. Explicit subject /
        # mask entries keep full control; --invert means the user wants plain
        # inverted luminance, so it bypasses the matte.
        mode = subject or ("auto" if not invert else None)
        coarse = load_glow(img, art_box, lcd_window, w, h, gamma, floor, invert,
                           subject=mode, name=name)
    # Palette. A painted/user mask wins as-is; sat modes are near-inverted so a
    # vivid-dominated palette would backfire -- keep the standard ramp for
    # luminance-ish modes, which is also the fallback's look.
    pal, pal_ambient = derive_palette(art, coarse, palette)
    if glow_color:
        pal[1] = tuple(glow_color)  # type: ignore[assignment]
        pal_ambient = tuple(min(255, int(v * 0.22)) for v in glow_color)
    # Manifest can override the computed ambient tint outright.
    amb = tuple(max(0, min(255, v)) for v in (ambient if ambient is not None else pal_ambient))
    return Asset(
        name=name, set_=set_, types=types, rarity=rarity,
        width=w, height=h, mask=pack_4bit(quantize_4bit(coarse)), palette=pal,
        ambient=amb,
    )


# --- output -----------------------------------------------------------------

def go_bytes(b: bytes, per_line: int = 12) -> str:
    lines = []
    for i in range(0, len(b), per_line):
        chunk = b[i:i + per_line]
        lines.append("\t\t" + " ".join(f"0x{v:02X}," for v in chunk))
    return "\n".join(lines)


def go_strings(ss: list[str]) -> str:
    return ", ".join('"%s"' % s.replace('"', '\\"') for s in ss)


def render_entry(a: Asset) -> str:
    pal = ",\n\t\t".join("RGB565(%d, %d, %d)" % (r, g, b) for (r, g, b) in a.palette)
    return f"""\t{{
\t\tName:    {go_strings([a.name])},
\t\tSet:     {go_strings([a.set_])},
\t\tTypes:   []string{{{go_strings(a.types)}}},
\t\tRarity:  {go_strings([a.rarity])},
\t\tMaskW:   {a.width},
\t\tMaskH:   {a.height},
\t\tMask: []byte{{
{go_bytes(a.mask)}
\t\t}},
\t\tPalette: []uint16{{
\t\t{pal},
\t\t}},
\t\tAmbient: RGB565({a.ambient[0]}, {a.ambient[1]}, {a.ambient[2]}),
\t}},"""


def render_go(assets: list[Asset]) -> str:
    body = "\n".join(render_entry(a) for a in assets)
    return f"""// Code generated by tools/make_card.py; DO NOT EDIT.
//
// The baked lightshow assets the CARD SHOW cartridge renders: coarse 4-bit
// "glow" masks (luminance of each card's art box, softened and quantized) and
// dark -> signature color -> white palettes. These are derived low-resolution
// luminance maps, not card images. The source images and this file are both
// gitignored; `make` regenerates this from assets/cards/ when present, and
// falls back to an original synthetic sample otherwise.

package cartridge

// cards is the baked library the CARD SHOW cartridge cycles through.
var cards = []CardAsset{{
{body}
}}
"""


def render_json(assets: list[Asset]) -> str:
    """The same assets as render_go, as JSON for the host sim.

    ``cmd/simui`` loads this at runtime so the interactive sim shows exactly the
    cards the firmware bakes, without a rebuild. The mask travels as base64 of
    the same packed 4-bit bytes; the palette as RGB triples.
    """
    doc = {
        "cards": [
            {
                "name": a.name,
                "set": a.set_,
                "types": a.types,
                "rarity": a.rarity,
                "mask_w": a.width,
                "mask_h": a.height,
                "mask_b64": base64.b64encode(a.mask).decode("ascii"),
                "palette": [[r, g, b] for (r, g, b) in a.palette],
                "ambient": [a.ambient[0], a.ambient[1], a.ambient[2]],
            }
            for a in assets
        ]
    }
    return json.dumps(doc, indent=2) + "\n"


def preview(a: Asset, path: str, w: int = 160, h: int = 128) -> None:
    """Write an upscaled, palette-colored view of the mask (sim-scale)."""
    img = Image.new("RGB", (w, h), (0, 0, 0))
    d = ImageDraw.Draw(img)
    for y in range(h):
        for x in range(w):
            mx = int(x * a.width / w)
            my = int(y * a.height / h)
            nib = a.mask[(my * a.width + mx) // 2]
            v = (nib >> 4) if ((my * a.width + mx) % 2 == 0) else (nib & 0xF)
            if v == 0:
                # Background wash: the ambient tint at the render floor.
                d.point((x, y), a.ambient)
                continue
            t = v / 15.0
            pos = t * (len(a.palette) - 1)
            i = min(len(a.palette) - 2, int(pos))
            f = pos - i
            c0, c1 = a.palette[i], a.palette[i + 1]
            d.point((x, y), tuple(int(c0[j] + (c1[j] - c0[j]) * f) for j in range(3)))
    img.save(path)


def slug(s: str) -> str:
    return "".join(c.lower() if c.isalnum() else "-" for c in s).strip("-")


def gofmt(src: str) -> str:
    """Pipe through gofmt if it is available, so the generated file is clean."""
    import shutil
    import subprocess

    if shutil.which("gofmt") is None:
        return src
    try:
        return subprocess.run(
            ["gofmt"], input=src, capture_output=True, text=True, check=True
        ).stdout
    except subprocess.CalledProcessError:
        return src


def sample_asset(args) -> Asset:
    return build_asset(
        make_sample_art(),
        art_box=None, lcd_window=None, grid=args.grid, gamma=1.0, floor=0.0,
        invert=False, mask_path=None, palette=args.palette,
        glow_color=None, subject=None, ambient=None,
        name="VOLTLET", set_="SAMPLE",
        types=["Electric"], rarity="COMMON",
    )


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("image", nargs="?", help="card art image (PNG/JPG/WebP)")
    ap.add_argument("--sample", action="store_true", help="use the built-in synthetic test art")
    ap.add_argument("--manifest", help="JSON list of card entries (multi-card build)")
    ap.add_argument("--art-box", type=int, nargs=4, metavar=("L", "T", "R", "B"),
                    help="art-box crop in source pixels")
    ap.add_argument("--lcd-window", type=float, nargs=4, metavar=("L", "T", "R", "B"),
                    help="region of the art box that sits over the LCD, as "
                         "fractions of the art box (default: all of it)")
    ap.add_argument("--lcd-fit", action="store_true",
                    help="centre a panel-sized window in the art box "
                         "(35.04x28.03 mm of a 63.5 mm card)")
    ap.add_argument("--grid", type=int, nargs=2, default=[40, 32], metavar=("W", "H"),
                    help="coarse mask resolution (default 40 32)")
    ap.add_argument("--gamma", type=float, default=1.0, help="glow gamma (default 1.0)")
    ap.add_argument("--floor", type=float, default=0.0, help="black floor 0..1 (default 0.0)")
    ap.add_argument("--invert", action="store_true",
                    help="glow where the art is dark (line-art / negative look)")
    ap.add_argument("--mask",
                    help="paint-your-own glow map in the source image's pixel "
                         "space (alpha channel, else grayscale); used instead of "
                         "the luminance glow")
    ap.add_argument("--subject", choices=("luma", "sat", "luma*sat", "auto"),
                    help="subject/background separation: luminance (default), "
                         "saturation, both, or a U2-Net salient-subject matte "
                         "(--subject auto; needs the rembg package, degrades "
                         "to luminance without it)")
    ap.add_argument("--ambient", type=int, nargs=3, metavar=("R", "G", "B"),
                    help="override the background wash tint (default: a dim "
                         "shade of the derived signature color)")
    ap.add_argument("--palette", type=int, default=4, help="palette entries (default 4)")
    ap.add_argument("--glow-color", type=int, nargs=3, metavar=("R", "G", "B"),
                    help="override the signature/glow color (default: most vivid "
                         "cluster). Useful when a card's background dominates.")
    ap.add_argument("--name", default="SAMPLE CARD")
    ap.add_argument("--set", dest="set_", default="SAMPLE")
    ap.add_argument("--types", nargs="*", default=["Colorless"])
    ap.add_argument("--rarity", default="COMMON")
    ap.add_argument("--preview", help="write an upscaled PNG preview (single card)")
    ap.add_argument("--preview-dir", help="write one preview PNG per card (manifest)")
    ap.add_argument("--format", choices=("go", "json"), default="go",
                    help="output format: Go source for the firmware (default) or "
                         "JSON for the host simulator")
    ap.add_argument("-o", "--out", default="-", help="output file, or - for stdout")
    args = ap.parse_args()

    assets: list[Asset] = []

    if args.manifest:
        with open(args.manifest) as f:
            data = json.load(f)
        entries = data["cards"] if isinstance(data, dict) else data
        base = os.path.dirname(os.path.abspath(args.manifest))
        for e in entries:
            path = os.path.join(base, e["file"])
            if not os.path.exists(path):
                print(f"make_card: skipping missing {path}", file=sys.stderr)
                continue
            mask = e.get("mask")
            assets.append(build_asset(
                Image.open(path).convert("RGB"),
                art_box=e.get("art_box"), lcd_window=e.get("lcd_window"),
                grid=e.get("grid", args.grid), gamma=e.get("gamma", args.gamma),
                floor=e.get("floor", args.floor), invert=e.get("invert", args.invert),
                mask_path=os.path.join(base, mask) if mask else None,
                palette=e.get("palette", args.palette), glow_color=e.get("glow_color"),
                subject=e.get("subject", args.subject), ambient=e.get("ambient"),
                name=e.get("name", "CARD"), set_=e.get("set", ""),
                types=e.get("types", ["Colorless"]), rarity=e.get("rarity", ""),
            ))
        if not assets:
            assets = [sample_asset(args)]
    elif args.sample:
        assets = [sample_asset(args)]
    elif args.image:
        assets = [build_asset(
            Image.open(args.image).convert("RGB"),
            art_box=args.art_box,
            lcd_window=("fit" if args.lcd_fit else args.lcd_window), grid=args.grid,
            gamma=args.gamma, floor=args.floor, invert=args.invert,
            mask_path=args.mask, palette=args.palette, glow_color=args.glow_color,
            subject=args.subject, ambient=list(args.ambient) if args.ambient else None,
            name=args.name, set_=args.set_, types=args.types, rarity=args.rarity,
        )]
    else:
        ap.error("provide an image, --sample, or --manifest")

    if _auto_fallbacks:
        print("make_card: subject=auto fell back to luminance for: "
              + ", ".join(_auto_fallbacks)
              + " (install rembg: python3 -m pip install rembg onnxruntime)",
              file=sys.stderr)

    if args.preview:
        preview(assets[0], args.preview)
    if args.preview_dir:
        os.makedirs(args.preview_dir, exist_ok=True)
        for a in assets:
            preview(a, os.path.join(args.preview_dir, slug(a.name or "card") + ".png"))

    out = render_json(assets) if args.format == "json" else gofmt(render_go(assets))
    if args.out == "-":
        sys.stdout.write(out)
    else:
        with open(args.out, "w") as f:
            f.write(out)
    print(f"make_card: wrote {len(assets)} card(s) to {args.out or 'stdout'}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
