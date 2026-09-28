# sycl-badge-tinygo

A minimal TinyGo firmware for the **SYCL Badge V2** — an RP2354B board
(48-GPIO RP2350B die, 2 MB in-package flash) running the SYCL 2026 production
(revision 2) hardware.

On boot it draws the **Go gopher** on the 160x128 LCD and holds it there.

> **Status: the display path is not yet verified on hardware.** The firmware
> builds cleanly, but the gopher has not been confirmed on the panel. See
> [Display (unverified)](#display-unverified) below.

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
- `display.go` — ST7735 setup for the badge's panel (SPI0) and the draw call.
- `gopher_data.go` — generated RGB565 image data (see below).
- `assets/gopher.png`, `tools/make_gopher.py` — the source image and the
  generator that produced `gopher_data.go`.

## Display (unverified)

The panel is a 160x128 ST7735S-class display on SPI0, wired as:

| Signal | GPIO |
| ------ | ---- |
| CS     | 17   |
| SCK    | 18   |
| MOSI   | 19   |
| DC     | 21   |
| BL     | 16   |

`display.go` uses `tinygo.org/x/drivers/st7735`. Two things are worth knowing:

- The panel's RESET line is tied to the RP2354B reset, so no reset GPIO is
  passed to the driver (`machine.NoPin`). This is a likely problem area: the
  driver calls `Configure()` on every pin it is given.
- The badge's backlight is a PWM pin (GPIO16); the driver drives it as a plain
  GPIO.

**This path has not been confirmed working on hardware.** A bare-GPIO blink
firmware is verified to run and print over USB-CDC, but the display build was
never observed drawing. Suspects, in order: `machine.SPI0.Configure` on RP2350,
the `NoPin` reset argument, or driver/pin setup. See the AI_USAGE note — treat
this as unvalidated.

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
make run-swd     # flash, reset, and run (recommended)
make flash-swd   # flash only - leaves the core halted
```

`make run-swd` holds the terminal open (it streams RTT/log output and must
stay attached), so run it in its own session; `make monitor` still works in
another terminal.

**`probe-rs download` writes flash but does not reset the chip.** The core is
left halted, so the program never starts: no LED, no USB enumeration, and
nothing for the monitor to read. If you see "no traffic" after flashing over
SWD, this is why — use `run-swd`.

**Watching serial output.** With the debug probe attached there are two USB
serial devices: the badge's own USB-CDC (the program's `fmt.Printf` output)
and the debug probe's UART bridge. `make monitor` selects the badge via
`-target`; if that ever picks wrong, pass `PORT=/dev/cu.usbmodemXXXX`.

**Probe firmware:** probe-rs 0.32 requires debug-probe firmware ≥ 2.2.0. Older
probes fail with *"firmware on the probe is outdated"*. Update from
<https://github.com/raspberrypi/debugprobe/releases>, or just use UF2, which
needs no extra tooling.

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
