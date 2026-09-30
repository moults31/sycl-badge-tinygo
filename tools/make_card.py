#!/usr/bin/env python3
"""Turn a card's art-box image into a baked lightshow asset for the badge.

The badge backlights a physical card laid on the LCD. Light diffuses through
the card stock, so the asset is a *coarse, soft luminance mask* (the "glow"
map) plus a small palette derived from the art -- not the art itself. Fine
detail would blur away, so the mask is tiny (a few hundred bytes) and gets
bilinearly upscaled on-device.

Inputs
------
- A card image supplied by the user (PNG/JPG), cropped to the art box. We do
  NOT bundle third-party card imagery. Bring your own.
- Or ``--sample``: an original, generated test creature, so the pipeline and
  the simulator can be exercised with no copyrighted input at all.

Output
------
A Go source file (package ``cartridge``) defining one ``CardAsset`` value:
a packed 4-bit mask, an RGB565 palette, and metadata.

Usage
-----
    # Own card image, art box pixels (left top right bottom):
    python3 tools/make_card.py card.png --art-box 40 30 360 470 \
        --name PIKACHU --set BASE --types Electric --rarity RARE \
        -o cartridge/card_data.go

    # Original synthetic test asset:
    python3 tools/make_card.py --sample --name VOLTLET --types Electric \
        -o cartridge/card_data.go --preview sim-out/card-asset-preview.png
"""

from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from typing import Iterable

from PIL import Image, ImageDraw

# Rec.709 luma weights.
LUMA = (0.2126, 0.7152, 0.0722)


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


# --- pipeline ---------------------------------------------------------------

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
        raise SystemExit(f"--lcd-window {tuple(win)} must be ordered fractions in 0..1")
    w, h = art.size
    box = (round(l * w), round(t * h), round(r * w), round(b * h))
    if box[0] >= box[2] or box[1] >= box[3]:
        raise SystemExit(f"--lcd-window {tuple(win)} is empty after rounding")
    return art.crop(box)


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


def downsample(gray: Image.Image, w: int, h: int) -> Image.Image:
    return gray.resize((w, h), Image.BOX)


def quantize_4bit(gray: Image.Image) -> list[int]:
    return [min(15, max(0, round(v * 15 / 255))) for v in gray.getdata()]


def pack_4bit(vals: list[int]) -> bytes:
    out = bytearray()
    for i in range(0, len(vals), 2):
        hi = vals[i] & 0xF
        lo = vals[i + 1] & 0xF if i + 1 < len(vals) else 0
        out.append((hi << 4) | lo)
    return bytes(out)


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
    dark = tuple(int(v * 0.35) for v in dark)  # unlit card reads as off
    return [dark, vivid, (255, 255, 255)]  # type: ignore[return-value]


# --- output -----------------------------------------------------------------

def go_bytes(b: bytes, per_line: int = 12) -> str:
    lines = []
    for i in range(0, len(b), per_line):
        chunk = b[i:i + per_line]
        lines.append("\t" + " ".join(f"0x{v:02X}," for v in chunk))
    return "\n".join(lines)


def go_strings(ss: list[str]) -> str:
    return ", ".join('"%s"' % s.replace('"', '\\"') for s in ss)


def render_go(a: Asset) -> str:
    pal = ",\n\t".join(
        "RGB565(%d, %d, %d)" % (r, g, b) for (r, g, b) in a.palette
    )
    return f"""// Code generated by tools/make_card.py; DO NOT EDIT.
//
// Single-card lightshow asset. The mask is a coarse 4-bit "glow" map derived
// from the card's art box; the palette runs dark -> bright. This file contains
// no third-party card imagery: regenerate it from your own card with the tool,
// or keep the original synthetic sample.
//
//   python3 tools/make_card.py <your-card.png> --art-box L T R B -o cartridge/card_data.go

package cartridge

// sampleCard is the baked asset the CARDSHOW cartridge renders.
var sampleCard = CardAsset{{
\tName:    {go_strings([a.name])},
\tSet:     {go_strings([a.set_])},
\tTypes:   []string{{{go_strings(a.types)}}},
\tRarity:  {go_strings([a.rarity])},
\tMaskW:   {a.width},
\tMaskH:   {a.height},
\tMask: []byte{{
{go_bytes(a.mask)}
\t}},
\tPalette: []uint16{{
\t{pal},
\t}},
}}
"""


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
            t = v / 15.0
            # Piecewise-linear palette lookup.
            pos = t * (len(a.palette) - 1)
            i = min(len(a.palette) - 2, int(pos))
            f = pos - i
            c0, c1 = a.palette[i], a.palette[i + 1]
            d.point((x, y), tuple(int(c0[j] + (c1[j] - c0[j]) * f) for j in range(3)))
    img.save(path)


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


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("image", nargs="?", help="card art image (PNG/JPG)")
    ap.add_argument("--sample", action="store_true", help="use the built-in synthetic test art")
    ap.add_argument("--art-box", type=int, nargs=4, metavar=("L", "T", "R", "B"),
                    help="art-box crop in source pixels")
    ap.add_argument("--lcd-window", type=float, nargs=4, metavar=("L", "T", "R", "B"),
                    help="region of the art box that sits over the LCD, as "
                         "fractions of the art box (default: all of it)")
    ap.add_argument("--grid", type=int, nargs=2, default=[40, 32], metavar=("W", "H"),
                    help="coarse mask resolution (default 40 32)")
    ap.add_argument("--gamma", type=float, default=1.0, help="glow gamma (default 1.0)")
    ap.add_argument("--floor", type=float, default=0.0, help="black floor 0..1 (default 0.0)")
    ap.add_argument("--palette", type=int, default=4, help="palette entries (default 4)")
    ap.add_argument("--name", default="SAMPLE CARD")
    ap.add_argument("--set", dest="set_", default="SAMPLE")
    ap.add_argument("--types", nargs="*", default=["Colorless"])
    ap.add_argument("--rarity", default="COMMON")
    ap.add_argument("--preview", help="write an upscaled PNG preview to this path")
    ap.add_argument("-o", "--out", default="-", help="output Go file, or - for stdout")
    args = ap.parse_args()

    if args.sample:
        img = make_sample_art()
    elif args.image:
        img = Image.open(args.image).convert("RGB")
    else:
        ap.error("provide an image or --sample")

    art = crop_art(img, args.art_box)
    art = crop_window(art, args.lcd_window)
    gray = glow_gray(art, args.gamma, args.floor)
    w, h = args.grid
    coarse = downsample(gray, w, h)
    vals = quantize_4bit(coarse)
    pal = derive_palette(art, coarse, args.palette)

    asset = Asset(
        name=args.name, set_=args.set_, types=args.types, rarity=args.rarity,
        width=w, height=h, mask=pack_4bit(vals), palette=pal,
    )

    if args.preview:
        preview(asset, args.preview)

    out = gofmt(render_go(asset))
    if args.out == "-":
        sys.stdout.write(out)
    else:
        with open(args.out, "w") as f:
            f.write(out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
