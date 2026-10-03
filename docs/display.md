# Display and backlight

The panel, the driver, and the two hardware bugs that shaped it. The
[README](../README.md) states the wiring and the fact that `display.go` is a
self-contained ST7735S driver; this page is the "why" behind the register
values, plus the two post-mortems from the first on-device tests.

## Panel and wiring

The panel is a **DT018BTFT-SHB**: a 1.8", 160×128, ST7735S-class display on
SPI0.

| Signal | GPIO |
| ------ | ---- |
| CS     | 17   |
| SCK    | 18   |
| MOSI   | 19   |
| DC     | 21   |
| BL     | 16   |

`display.go` is small and self-contained, and it does **not** use
`tinygo.org/x/drivers/st7735`. Its init sequence and register values are copied
from the badge's reference firmware (`src/os/drivers/lcd.zig`, `init_display()`),
because this panel needs panel-specific power/gamma settings rather than the
generic ST7735 init the driver sends.

Other wiring notes:

- The panel's RESET line is tied to the RP2354B reset, so there is no reset GPIO;
  the driver just waits for the power-on reset to settle.
- SPI0 is write-only. `SDI` is set to `machine.NoPin`, which matches the hardware
  (no MISO) and prevents the SPI peripheral from claiming GPIO0.
- The backlight on GPIO16 is the enable input of the TPS61041 boost converter,
  not an LED anode — see [Backlight](#backlight) below.

## Orientation, and the red/blue post-mortem

The panel is natively 128×160, but the driver addresses it as a **160×128
landscape canvas**:

- `MADCTL = 0x68` (`MX | MV | BGR`). `MV` (row/column exchange) makes the column
  address space 160 wide; `BGR` makes the panel read standard-RGB565 frames in
  the order it is actually wired (see below).
- Draws then use `CASET = 0..159`, `RASET = 0..127`.

**Post-mortem: why `MV` and `BGR`.** The first version of this driver used
`tinygo.org/x/drivers/st7735` at rotation 0 (`MADCTL = 0xC0`, no `MV`) while
still addressing 160 columns. Without `MV` the column (source) axis is only 128
deep, so every 160-pixel-wide write overran the display RAM, wrapped, and
produced streaky diagonal garbage on the panel. It also left the driver's `SDI`
at its zero value (GPIO0) and relied on the generic init.

The first on-device test of the card art mode then reported a colour problem that
the host sim did not show: a red card (Electrode's body, sim RGB(216,56,72)) read
**blue**, and a blue card (Horsea's body, sim RGB(0,176,208)) read **yellow**.
Both are exactly the red/blue channel swap of the source art, which the sim
reproduces faithfully. Cause: the DT018BTFT panel is wired **BGR**, but `MADCTL`
was left at `0x60` (`MX | MV`, RGB order). Every frame is standard RGB565 —
`RGB565()` puts red in bits 15..11, `tools/make_gopher.py` emits high-byte-first
RGB565, and the host sim decodes the same layout — so the panel's RGB
interpretation swapped R and B on screen. This affected every cart; it was simply
invisible in the plasma gradients and zeroman sprite palettes. Fix: set the `BGR`
bit, `MADCTL = 0x68`. The image data stays standard RGB565, high byte first.

Because a different panel/batch or a stale worktree could regress this, CARD SHOW
keeps a runtime colour-mapping cycle on the joystick **Click** (unused
elsewhere); see [cards.md](cards.md).

## Backlight

The backlight is on `BKLT` (GPIO16), driven by PWM (RP2350 slice 0, channel A,
~1 kHz — matching the reference firmware's `clk_div=150, wrap=1023`). GPIO16 is
the **enable input of the TPS61041 backlight boost converter** (`BKLT_EN` in the
badge schematic): the boost's FB node is strapped to the LED sense rail by a
solder jumper, so the duty cycle gate-modulates the LED rail and the panel reads
duty as brightness. The LED rail reaches the panel through the FFC, not this
GPIO directly — so the effective "on" threshold sits near full duty, and any
dimming has to run through this one gate.

`display.go` sets `frontlightPWMPeriod = 1_000_000` (nanoseconds, ~1 kHz).
`lcdSetBacklight(level)` maps 0..255 onto the PWM's `Top()`; the runtime restores
full brightness at the start of every frame unless a cart lowers it.

**Post-mortem: why 1 kHz, not 200 kHz.** The first on-device test of the card
lightshow showed the image for < 1 s, then a fully dark panel for a
manual-stopwatch ~8 s, repeating — while the host sim showed a steady glow.
Cause: the backlight breathe ran faster than the boost converter can follow. The
driver had set the GPIO16 PWM period to 5 µs (~200 kHz). A ~200 kHz chop mostly
falls below what the converter needs to keep the rail up: only duty near full
actually lights, and any lower duty reads as off — even though the sim (which
models only the framebuffer and a duty number) shows a steady glow. CARD SHOW
simultaneously makes the picture itself near-black during those troughs (ambient
≈ RGB(56,26,6), and 50–75 % of each face's mask sits at nibble 0), so "dim
backlight" and "black picture" arrived together. Net effect, exactly as
reported: a brief lit flash while duty rode the top of the breath, then a long
fully-dark stretch. Fix: drive the backlight the way the reference firmware does
— `clk_div=150, wrap=1023`, i.e. ~1 kHz — which `display.go` now does. At 1 kHz
the duty encodes brightness on this boost the way the panel expects. Verified
against the reference firmware's `src/os/drivers/lcd.zig` and the badge's
`kicad/v2` schematic.

A consequence for carts: turning breathing off holds the backlight at **full**
rather than a fixed intermediate duty, because the boost only lights predictably
at full duty and an intermediate duty drifts. "Steady" means "full".

## Writing a frame

`lcdPresent(frame)` flushes a full 160×128 standard-RGB565 frame (row-major,
index `y*160+x`) to the panel in one windowed write, high byte first. It reuses a
package-level `lcdRowBuf [panelWidth * 2]byte` so presenting a frame allocates
nothing. It is the single SPI touch point for the cartridge runtime.
