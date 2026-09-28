# sycl-badge-tinygo

A minimal TinyGo firmware for the **SYCL Badge V2** — an RP2354B board
(48-GPIO RP2350B die, 2 MB in-package flash) running the SYCL 2026 production
(revision 2) hardware.

On boot it draws the **Go gopher** on the 160x128 LCD and holds it there.

> **Status: the display path is not yet confirmed on hardware.** The firmware
> builds cleanly and the panel init now mirrors the known-good reference
> firmware, but the gopher has not been re-tested on the panel since the fix.
> See [Display](#display) below.

> **This is a hobby project.** Code here is largely AI-generated and not
> guaranteed to be human-reviewed. See [AI_USAGE.md](AI_USAGE.md) before
> flashing anything.

## Quick start

```sh
make build      # -> hello.uf2 + hello.elf
make flash      # copy hello.uf2 to the badge's BOOTSEL drive
make monitor    # watch the USB-CDC serial output
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
- `main.go` — the program: initialise the display, draw the gopher, idle.
- `display.go` — self-contained ST7735S driver for the badge's panel (SPI0):
  the panel init sequence and the draw calls.
- `gopher_data.go` — generated RGB565 image data (see below).
- `assets/gopher.png`, `tools/make_gopher.py` — the source image and the
  generator that produced `gopher_data.go`.

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
- The backlight is on `BKLT` (GPIO16). It is driven high as a plain GPIO, which
  the reference uses as full brightness (its PWM `set_level`). The panel's
  `TFT_LITE` rail is tied to VBUS.

If red and blue come out swapped on hardware, set the `BGR` bit in `MADCTL`
(i.e. `0x68` instead of `0x60`); the image data is standard RGB565 (high byte
first), which is what `tools/make_gopher.py` emits.

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

The user LED (GPIO14) blinks at 2 Hz, and USB-CDC serial prints one line every
250 ms:

```
hello from SYCL Badge V2 #0
hello from SYCL Badge V2 #1
...
```

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
