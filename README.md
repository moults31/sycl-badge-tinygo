# sycl-badge-tinygo

A minimal TinyGo firmware for the **SYCL Badge V2** — an RP2354B board
(48-GPIO RP2350B die, 2 MB in-package flash) running the SYCL 2026 production
(revision 2) hardware.

On boot it shows a **cartridge menu** on the 160x128 LCD. Press **A** to launch
a cartridge — a `PLASMA` effect, a `ZEROMAN` platformer, a `PANIC TEST`
diagnostic, and a `CARD SHOW` lightshow — and hold **Start+Select** for 250 ms
to return to the menu. Cartridges are Go values compiled into this one
firmware; see [Cartridge runtime](#cartridge-runtime).

> **This is a hobby project.** Code here is largely AI-generated and not
> guaranteed to be human-reviewed. See [AI_USAGE.md](AI_USAGE.md) before
> flashing anything.

## Quick start

```sh
make build      # -> hello.uf2 + hello.elf
make flash      # copy hello.uf2 to the badge's BOOTSEL drive
make flash-swd  # or flash over SWD with a Debug Probe + OpenOCD
make monitor    # watch the USB-CDC serial output
make sim        # run the cartridge runtime on the host, render PNG frames
make sim-ui     # run it interactively in a browser window (keyboard + buttons)
make test       # host-side unit tests for the cartridge runtime
```

## How it works

TinyGo ships an abstract `rp2350b` target (the B-variant die) but no concrete
board for it. A concrete board must supply the `machine` package constants the
RP2350 needs (`xoscFreq`, UART/SPI/I2C default pins, USB IDs).

- `targets/sycl-badge-v2.json` — custom target: inherits `rp2350b`, sets the
  `sycl_badge_v2` build tag, USB CDC serial, `firmware.uf2`.
- `targets/board_sycl_badge_v2.go` — board constants, gated on
  `//go:build sycl_badge_v2`. Pin map mirrors the reference firmware's
  `src/board_v2.zig`.
- `main.go` — the program: initialise the display and run the cartridge runtime.
- `display.go` — self-contained ST7735S driver for the badge's panel (SPI0):
  the panel init sequence and the full-frame flush (`lcdPresent`).
- `hw.go`, `env_hw.go` — adapt the panel and the buttons to the runtime's
  `Display`/`Env` interfaces.
- `cartridge/` — the hardware-independent runtime: the `Cartridge` interface,
  the menu / launch / `recover` loop, and the carts. It imports no hardware, so
  it is unit-tested and simulated on the host.
  - `plasma.go` — the plasma effect.
  - `pokecard.go` — the card art-box shinethrough lightshow (`CARD SHOW`); its
    `cards_data.go` asset is generated at build time and gitignored (see
    [Card shinethrough lightshow](#card-shinethrough-lightshow)).
  - `zeroman*.go` — the platformer port (types, player, enemies, effects, the
    game loop) plus generated `zeroman_gfx.go` and `zeroman_stage.go`.
  - `panictest.go` — the phase-1 recover diagnostic.
- `cmd/sim` — the host simulator: replays a scripted scenario and writes PNG
  frames (`make sim`).
- `cmd/simui` — the interactive simulator: runs the same runtime live and serves
  the panel and controls to a browser window, driven by keyboard or on-screen
  buttons (`make sim-ui`). It loads the user's real cards from the shared card
  library at startup.
- `tools/make_card.py` — generates the baked card lightshow asset from a card's
  art box (or an original synthetic sample).
- `tools/cards_dir.sh` — prints the shared card library every worktree resolves
  to (see [One card library across worktrees](#one-card-library-across-worktrees)).
- `tools/make_zeroman_gfx.py`, `tools/make_zeroman_stage.py` — generate the
  zeroman art (RGB565 palettes + packed indices) and stage data from the
  reference cart.
- `gopher_data.go` — generated RGB565 image data (see below).
- `assets/gopher.png`, `tools/make_gopher.py` — the source image and the
  generator that produced `gopher_data.go`.

## Cartridge runtime

The runtime swaps Go-valued cartridges inside the one firmware. It ports the
*contract* from the Zig reference firmware, not its loader: there is no UF2
loader, no second core, and no second TinyGo runtime.

- A cart implements `Start(p *Platform)` (once per launch) and
  `Update(p *Platform)` (once per frame).
- The platform owns machine init, the ST7735S driver, button polling, and the
  same-core SPI flush of a package-level 160x128 RGB565 backbuffer. It presents
  the frame after every `Update`; present is a function call.
- Cart state lives on the cart struct and is rebuilt in `Start` — a fresh
  `Start` on every launch, which replaces the Zig BSS wipe. Carts touch no
  `machine` state.
- `Update` runs inside `recover`: a Go `panic` returns to the menu without
  re-initialising the panel. A hard fault or an infinite loop still takes the
  chip down; that is accepted.
- Hold **Start+Select** for 250 ms to exit a cart. The chord is edge-triggered,
  so a chord held across a launch does not bounce straight back. **A** launches
  the highlighted cart; joystick up/down moves the selection.

The runtime lives in `cartridge/` and is deliberately hardware-free, so the
menu, launch/exit, fresh-`Start`, and panic-recovery paths are covered by
`make test` and rendered by `make sim`:

```sh
make test   # go test ./cartridge/...
make sim    # writes sim-out/01-menu.png, 03-plasma-later.png, ...
```

`make sim` renders the menu, plasma at two points in time, the menu after the
exit chord, a relaunch that is byte-identical to the first plasma frame (proof
of a fresh `Start`), the zeroman title and playfield, the menu after the
panic cart is recovered, and the card lightshow — one frame per baked card,
then a breathing frame, an attack flash, and the calibration overlay.

### Interactive simulator

`make sim-ui` runs the runtime live instead of scripted: it starts a small HTTP
server on `127.0.0.1`, opens a browser window, and drives the very same
`cartridge.Runner` the badge runs, at the same ~60 fps pace. The window shows
the 160x128 panel at an integer zoom and posts the state of its keyboard and
on-screen controls back as the runner's `Env`, so the menu, the launch/exit
chords, panic recovery, and every cart behave exactly as they do on hardware —
only the panel and the pins are replaced.

- **Keyboard (preferred):** arrows or WASD move the joystick, **Z**/**J** is A,
  **X**/**K** is B, **Enter** is Start, **Shift** is Select, **C** is the stick
  click, **Esc** releases everything. Hold **Start+Select** for 250 ms to leave
  a cart.
- **On-screen buttons:** the same controls, pressed for as long as the pointer
  is held, for touch or when a keyboard is not to hand.
- The green dot shows the live frame stream; the backlight percentage follows
  the carts that dim or pulse the panel (e.g. the card lightshow).

The sim shows your **real cards** without a rebuild. At startup it runs the very
same `tools/make_card.py` the firmware build uses, on the shared card library
(see [One card library across worktrees](#one-card-library-across-worktrees)),
and loads the assets it emits — so dropping a card in and relaunching the sim is
enough. The banner reports which set was loaded; with no manifest (or no
Python/Pillow) it falls back to the cards already baked into the binary. The
firmware still bakes its cards at build time through `make build`/`card-data`, so
both use one generator. In CARD SHOW, **left/right (or A/D) cycle the loaded cards**;
Select's overlay shows the current name and `n/total` index.

Extra flags pass through, e.g.
`make sim-ui SIMUI_FLAGS="-addr 127.0.0.1:8423 -open=false"` or
`make sim-ui SIMUI_CARDS=~/my-cards`, or run the command directly with
`go run ./cmd/simui`. Like `make sim`, it needs the generated card data, which
`make sim-ui` builds first.

### One card library across worktrees

Card imagery is gitignored, so a git worktree never owns a copy: images dropped
into one worktree are invisible to the others and are lost when that worktree is
removed. To avoid that, both `make` and `cmd/simui` resolve **one** shared card
library, in this order:

1. `$SYCL_CARDS_DIR`, if set — point it anywhere (a backup, a NAS, a shared
   folder);
2. the **main worktree's** `assets/cards` — git's common dir lives in the main
   checkout, so every linked worktree shares that one library;
3. `./assets/cards` in the current worktree (legacy / standalone clone).

So the rule of thumb is: **put card images and `manifest.json` in the main
checkout's `assets/cards/`**, and every worktree — plus the firmware built from
any of them — sees them. `tools/cards_dir.sh` prints the resolved directory;
`make cards-dir` shows it, and `make cards-link` symlinks the current worktree's
`assets/cards` to it for tools that expect the documented path.

```sh
make cards-dir                  # where the shared library resolves to
cp ~/Downloads/my-card.webp "$(make -s cards-dir)/"   # add a card
# ... add an entry to "$(make -s cards-dir)/manifest.json", then:
make sim-ui                     # picks it up on relaunch
```

`SYCL_CARDS_DIR=/path/to/cards make sim-ui` (or `make build`) uses an alternate
library without moving anything into the repo.

### Card shinethrough lightshow

`CARD SHOW` (`cartridge/pokecard.go`) backlights a physical card laid on the
LCD. Light diffuses through the card stock, so the show does not reproduce the
art — it drives a coarse, soft **glow mask** derived from the art box, tinted
by a small palette and animated as breathing, a sweeping holo band, drifting
sparkles, and an A-button flash. The whole panel's intensity also breathes
through the backlight PWM. **Left/Right** cycle the baked card library;
**Select** toggles the alignment overlay (which shows the card name and its
`n/total` library index); **A** fires the attack flash.

Because diffusion washes out fine detail, each asset is tiny (a 4-bit mask
stretched across the panel and bilinearly upscaled). Assets are **generated at
build time and gitignored** — no card imagery and no derived mask is committed.
`make build`/`size`/`test`/`sim` run `tools/make_card.py`:

- if the shared library's `manifest.json` exists, it bakes one `CardAsset` per
  entry (the shared library is the main worktree's `assets/cards/`, or
  `$SYCL_CARDS_DIR`; see
  [One card library across worktrees](#one-card-library-across-worktrees));
- otherwise it emits a single **original synthetic sample**, so a fresh clone
  and CI still compile (CI installs Pillow for this).

Masks separate the card's **subject from its background** automatically: by
default the generator runs a U2-Net saliency matte (`rembg`, CPU) over the art
box and gates the stretched luminance by that silhouette, so the creature
glows out of a near-black ambient wash instead of rendering a flat luminance
photo. Fallbacks, no per-card config needed:

- `rembg`/`onnxruntime` not installed (or the model download fails) -> plain
  stretched luminance, with a `make_card:` warning naming the affected cards;
- `"subject": "luma" | "sat" | "luma*sat"` in a manifest entry -> the manual
  derivations (`sat` = color-saturation channel, percentile-stretched);
- `"invert": true` -> inverted luminance (line-art look), bypassing the matte;
- `"mask": "file.png"` -> your own painted glow/silhouette, always winning.

The bake also derives an `ambient` tint per card (a dim shade of the
signature color; overridable via `"ambient": [r, g, b]`). The render keeps
that wash moving slowly under the subject while breathing it with the global
envelope, so foreground and background stay separated even as everything
diffuses.

Source images live under the gitignored shared library. A manifest entry:

```json
{"file": "charizard.webp", "name": "CHARIZARD", "set": "CLASSIC",
 "types": ["Fire"], "art_box": [22, 96, 378, 295],
 "lcd_window": "fit", "glow_color": [255, 120, 30]}
```

Single-card CLI:

```sh
python3 tools/make_card.py assets/cards/your-card.webp --art-box L T R B \
    --lcd-fit --glow-color 255 120 30 --name CHARIZARD --set CLASSIC \
    --types Fire -o cartridge/cards_data.go --preview-dir sim-out/card-previews
```

Key flags (CLI or manifest fields):

- `art_box: L T R B` — the illustration window in source pixels.
- `lcd_window: l t r b`, or `"fit"` — the part of the art box that sits over the
  panel. The panel (35×28 mm) is smaller than the art box, so cropping to it
  makes the glow line up 1:1 with the print instead of showing a scaled-down
  picture of the whole art. `"fit"` centres a panel-sized window automatically;
  it is the one measurement the mechanical build will refine.
- `subject` - `auto` (default: U2-Net matte, see above), `luma`, `sat`, or
  `luma*sat`; `--subject` is the single-card CLI equivalent.
- `glow_color: R G B` — override the signature color (a busy card background can
  otherwise dominate the auto-picked color).
- `ambient: R G B` - override the background wash tint (default: a dim shade of
  the signature color).
- `invert` — glow where the art is *dark* (a line-art/negative look that lights
  up a card's outlines and features).
- `mask: file` — paint your own glow/silhouette over the card image (alpha or
  grayscale) and use it instead of luminance; the way to get a true silhouette
  when the auto matte picks the wrong region.
- `gamma`/`floor` — contrast shape of the derived glow.

Calibration is per card and per session (RAM only). The overlay draws the card
name, a border, a crosshair, and a rotation tick; the joystick nudges the mask
and **A**/**B** step the rotation until the glow sits under the printed art.

### Zeroman

`zeroman*.go` is a port of `showcase/carts/zeroman` at 160x128. The reference's
package-level state (`GameData`, the player, enemies, room transition, text
layer, RNG) lives on the cart struct and is rebuilt in `Start`. Tiles and
sprites are generated RGB565 with packed palette indices, and the stage is
parsed from the reference's `needleman.zig`, both through `go generate`-style
tools (`tools/make_zeroman_gfx.py`, `tools/make_zeroman_stage.py`). The
reference's Tracy zones are dropped. Input is the platform's button snapshot;
the cart does not touch timers, SPI or `machine`.

### Hardware self-test

For bring-up without buttons, `-tags=cartdemo` swaps the real button
environment for a scripted one that replays the simulator's sequence on the
panel and logs each transition over USB-CDC:

```sh
tinygo build -target=targets/sycl-badge-v2.json -tags=cartdemo -o demo.elf .
```

`PANIC TEST` remains as a diagnostic cartridge: it launches, then panics on its
first `Update`, so the recover path can still be exercised on hardware.

## Display

The panel is a **DT018BTFT-SHB**: a 1.8", 160x128, ST7735S-class display on
SPI0, wired as:

| Signal | GPIO |
| ------ | ---- |
| CS     | 17   |
| SCK    | 18   |
| MOSI   | 19   |
| DC     | 21   |
| BL     | 16   |

`display.go` is a small, self-contained ST7735S driver. Its init sequence and
register values are copied from the badge's reference firmware
(`src/os/drivers/lcd.zig`, `init_display()`), because this panel needs the
panel-specific power/gamma settings rather than the generic ST7735 init that
`tinygo.org/x/drivers/st7735` sends.

The important detail is **orientation**. The panel is natively 128x160; the
driver addresses it as a 160x128 landscape canvas exactly as the reference does:

- `MADCTL = 0x60` (`MX | MV`). The `MV` bit exchanges the row/column axes, which
  makes the column address space 160 wide.
- Draws then use `CASET = 0..159`, `RASET = 0..127`.

The first version of this driver used `tinygo.org/x/drivers/st7735` at
rotation 0 (`MADCTL = 0xC0`, no `MV`) while still addressing 160 columns. Without
`MV` the column (source) axis is only 128 deep, so every 160-pixel-wide write
overran the display RAM, wrapped, and produced the streaky garbage seen on the
panel. It also left the driver's `SDI` at its zero value (GPIO0) and relied on
the generic init.

Other notes:

- The panel's RESET line is tied to the RP2354B reset, so there is no reset GPIO;
  the driver just waits for the power-on reset to settle.
- SPI0 is write-only. `SDI` is set to `machine.NoPin` — this both matches the
  hardware (no MISO) and prevents the SPI peripheral from claiming GPIO0.
- The backlight is on `BKLT` (GPIO16), driven by **PWM** (RP2350 slice 0,
  channel A, ~1 kHz — matching the reference firmware's `clk_div=150,
  wrap=1023` backlight slice). GPIO16 is the **enable input of the TPS61041
  backlight boost converter** (`BKLT_EN` in the badge schematic): the boost's
  FB node is strapped to the LED sense rail by a solder jumper, so the duty
  cycle gate-modulates the LED rail and the panel reads duty as brightness.
  The LED rail reaches the panel through the FFC, not this GPIO directly —
  so the effective "on" threshold sits near full duty, and any dimming has to
  run through this one gate (see "CARD SHOW went black" below for what a
  too-fast carrier does).

If red and blue come out swapped on hardware, set the `BGR` bit in `MADCTL`
(i.e. `0x68` instead of `0x60`); the image data is standard RGB565 (high byte
first), which is what `tools/make_gopher.py` emits.

### CARD SHOW went black on real hardware

Symptom, reported from the first on-device test of the card lightshow: the
image (the lit card mask) appears for < 1 s, the panel goes fully dark for a
manual-stopwatch ~8 s, then it lights again — repeating. The host sim shows a
steady glow the whole time.

Cause: the backlight breathe ran faster than the backlight's boost converter
can follow. The driver set the GPIO16 PWM period to 5 µs (~200 kHz). GPIO16 is
not an LED anode: it is the **enable of the TPS61041 backlight boost**, whose
feedback node is strapped to the LED sense rail. A ~200 kHz chop mostly falls
below what the converter needs to keep the rail up: only duty near full
actually lights, and any duty below that reads as off — even though the sim
(which models only the framebuffer and a duty number) shows a steady glow.
CARD SHOW then makes the picture itself near-black during those troughs
(ambient ≈ RGB(56,26,6), and 50–75 % of each face's mask sits at nibble 0),
so "dim backlight" and "black picture" arrive together. Net effect, exactly as
reported: a brief lit flash while duty rides the top of the breath, then a
long fully-dark stretch — the sim's breath is one ~3 s cycle at 60 fps, and on
hardware the converter's enable/settle behaviour stretched the dark phase to
the ~8 s the stopwatch caught.

Fix: drive the backlight the way the reference firmware does —
`clk_div=150, wrap=1023`, i.e. ~1 kHz at 150 MHz — which is what `display.go`
now does (`frontlightPWMPeriod = 1_000_000`). At 1 kHz the duty encodes
brightness on this boost the way the panel expects, and the lightshow's
ambience reads as ambience again. (Verified against the reference firmware's
`src/os/drivers/lcd.zig` and the badge's `kicad/v2` schematic.)

### Regenerating the image

```sh
python3 tools/make_gopher.py assets/gopher.png > gopher_data.go
```

### Image credit

The Go gopher was designed by **Renée French** and is licensed under the
**Creative Commons 3.0 Attribution license (CC BY 3.0)**:
<https://go.dev/wiki/Gopher>. The source PNG is from the Go project's website
repository. Attribution is carried in the generated `gopher_data.go` header and
must be preserved.

### The `$TINYGOROOT` copy step

TinyGo resolves board definitions from `$TINYGOROOT/src/machine`, and a custom
target's `extra-files` only accepts C/assembly — there is no way to inject a Go
board file from a project-local path. So `make build` syncs
`targets/board_sycl_badge_v2.go` into the TinyGo install first (`make
install-board`). The repo is the source of truth; the toolchain is a build
scratch space, re-synced on every build.

## Prerequisites

- TinyGo 0.42.0 or newer, with Go 1.25–1.27 on `PATH` (TinyGo reuses the
  system Go toolchain).
  - Homebrew: `brew tap tinygo-org/tools && brew install tinygo`
  - Or the release tarball from
    <https://github.com/tinygo-org/tinygo/releases> (macOS Homebrew's bottle
    may refuse to install if your Xcode is older than the bottle expects; the
    tarball avoids that).
- The badge connected over USB.

## Build and flash

The badge exposes a UF2 mass-storage bootloader. The volume (`RP2350`) is only
mounted in BOOTSEL mode — hold `RESET` + `BOOT_SEL`, release `RESET`, then
release `BOOT_SEL`.

```sh
make build      # build hello.uf2 and hello.elf
make size       # flash/RAM usage
make flash      # copy hello.uf2 to /Volumes/RP2350
make monitor    # tinygo monitor
make clean      # remove build artifacts
```

The volume unmounts and the badge reboots into the new firmware immediately.

## Verify

On boot the panel shows the cartridge menu, with `PLASMA` highlighted. Press
**A** to run the plasma effect; hold **Start+Select** (250 ms) to return; press
**A** again for a fresh start. Push the joystick down to select `ZEROMAN` and
press **A**: press a direction or **A** to leave the title, then move with the
joystick and jump (**A**) / shoot (**B**). Push down to `PANIC TEST` and press
**A**: it panics in `Update` and the menu returns with a `PANIC:` line, proving
the recover path.

USB-CDC logs each lifecycle transition (`launch: …`, `exit: start+select`,
`recovered update panic: …`), but only once a serial reader is attached — the
TinyGo USB-CDC drops writes until the host asserts DTR, so boot prints usually
appear only if `make monitor` was already running.

On macOS the port looks like `/dev/cu.usbmodem*`. `make monitor` finds it:

```sh
make monitor                              # uses this target's VID:PID
make monitor PORT=/dev/cu.usbmodemXXXX    # or pick the port explicitly
```

`make monitor` passes `-target`, which matters: the badge's USB VID:PID
(`2e8a:000a`) is shared with other RP2350 boards, so without `-target` TinyGo
sees every USB serial port (the badge *and* the debug probe's UART bridge) and
refuses to guess:

```
multiple serial ports available - use -port flag, available ports are
/dev/cu.usbmodem11402, /dev/cu.usbmodem2101
```

You can always bypass TinyGo's monitor and read the port directly:

```sh
cat /dev/cu.usbmodemXXXX
```

## Flashing over SWD (optional)

A Raspberry Pi Debug Probe on the badge's SWD port can load over the debug
interface instead of UF2:

```sh
make flash-swd   # flash, verify, reset, and run over SWD
make run-swd     # alias for flash-swd (kept for compatibility)
```

Unlike UF2, no BOOTSEL/RESET button presses are needed. The OpenOCD command is
self-contained — it flashes, verifies, resets, and releases the core running —
so the program starts and `make monitor` works immediately afterwards.

### Why OpenOCD and not probe-rs

`flash-swd` drives **OpenOCD**, not probe-rs. probe-rs 0.32 is not usable for
iteration on this board:

- `probe-rs download` writes flash but then deliberately **leaves the RP2350
  core halted**. The program never starts: no LED and no USB enumeration.
- `probe-rs reset` and even `probe-rs download --reset` do **not** recover it —
  the reset is a reset-halt and the core is never resumed. Once a halt is left
  behind, probe-rs's chip auto-detection stops working too (*"The connected
  chip could not automatically be determined"*).
- probe-rs's built-in `RP235x` target declares a 64 MiB NVM window, which does
  not match the badge's 2 MB QSPI flash.

OpenOCD's `program … verify reset exit` does not leave the core halted, and
identifies the flash correctly:

```
Info : RP2350 rev 3, QSPI Flash win w25q16jv id = 0x1540ef size = 2048 KiB in 512 sectors
** Verified OK **
** Resetting Target **
```

### Installing OpenOCD

Homebrew's stable `open-ocd` (0.12.0) predates the RP2350 and does not ship
`target/rp2350.cfg`. Install a build that does:

```sh
brew install open-ocd --HEAD        # upstream git, which has rp2350.cfg
```

The Raspberry Pi fork (`raspberrypi/openocd`) works too, as do the OpenOCD
binaries bundled with the `raspberrypi.pico-vscode` extension.

### Cables and reset

The Debug Probe's 3-pin SWD connector carries only `SWCLK`, `SWDIO`, and `GND`
— there is **no `nRESET` line**. OpenOCD therefore resets the RP2350 through
SWD (the chip's ROM/debug reset), so `--connect-under-reset` is neither used
nor needed. `make flash-swd` also recovers a board whose previous flash left
the core halted or hard-faulted, with no button presses.

### Watching serial output

With the debug probe attached there are two USB serial devices: the badge's own
USB-CDC (the program's `fmt.Printf` output) and the debug probe's UART bridge.
`make monitor` selects the badge via `-target`; if that ever picks wrong, pass
`PORT=/dev/cu.usbmodemXXXX`.

### Probe firmware

OpenOCD prints the probe firmware as `CMSIS-DAP: FW Version = …` (2.0.0 on the
probe used here, which works). Update from
<https://github.com/raspberrypi/debugprobe/releases> if needed.

## Continuous integration

`.github/workflows/ci.yml` runs on push and pull request:

- **build** — installs TinyGo and runs `make build` + `make size`, uploading
  the `.uf2`/`.elf` artifacts. `tinygo build` is the real compile gate, since
  the `machine` package cannot be resolved by a stock Go toolchain.
- **format** — `gofmt -l` must be empty. (Generic Go linters such as
  golangci-lint are not used: they cannot type-check any file that imports
  `machine`.)
- **scan** — Trivy filesystem scan (CRITICAL/HIGH) and a gitleaks secret scan.

## License

MIT — see [LICENSE](LICENSE).
