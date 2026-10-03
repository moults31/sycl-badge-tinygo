# Toolchain and bring-up

Notes that matter when building, flashing by wire, or debugging without hands.
The [README](../README.md) keeps the flashing instructions and prerequisites
inline; this page is the rationale and the deeper options.

## The custom target and the `$TINYGOROOT` copy step

TinyGo ships an abstract `rp2350b` target (the B-variant die) but no concrete
board for it. A concrete board must supply the `machine` package constants the
RP2350 needs (`xoscFreq`, UART/SPI/I2C default pins, USB IDs).

- `targets/sycl-badge-v2.json` — custom target: inherits `rp2350b`, sets the
  `sycl_badge_v2` build tag, USB CDC serial, `firmware.uf2`.
- `targets/board_sycl_badge_v2.go` — board constants, gated on
  `//go:build sycl_badge_v2`. The pin map mirrors the reference firmware's
  `src/board_v2.zig`.

TinyGo resolves board definitions from `$TINYGOROOT/src/machine`, and a custom
target's `extra-files` only accepts C/assembly — there is no way to inject a Go
board file from a project-local path. So `make build` syncs
`targets/board_sycl_badge_v2.go` into the TinyGo install first
(`make install-board`). **The repository is the source of truth; the toolchain is
a build scratch space, re-synced on every build.**

## One card library across worktrees

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

## Hardware self-test without hands (`-tags=cartdemo`)

For bring-up without buttons, the `cartdemo` build tag swaps the real button
environment for a scripted one (`env_demo.go`) that replays the simulator's
sequence on the panel and logs each transition over USB-CDC:

```sh
tinygo build -target=targets/sycl-badge-v2.json -tags=cartdemo -o demo.elf .
```

`PANIC TEST` remains as a diagnostic cartridge: it launches, then panics on its
first `Update`, so the recover path can still be exercised on hardware (and in
`make sim`).

## Flashing over SWD, and why OpenOCD and not probe-rs

A Raspberry Pi Debug Probe on the badge's SWD port can load over the debug
interface instead of UF2:

```sh
make flash-swd   # flash, verify, reset, and run over SWD
make run-swd     # alias for flash-swd (kept for compatibility)
```

Unlike UF2, no BOOTSEL/RESET button presses are needed. The OpenOCD command is
self-contained — it flashes, verifies, resets, and releases the core running —
so the program starts and `make monitor` works immediately afterwards.

`flash-swd` drives **OpenOCD**, not probe-rs. probe-rs 0.32 is not usable for
iteration on this board:

- `probe-rs download` writes flash but then deliberately **leaves the RP2350
  core halted**. The program never starts: no LED and no USB enumeration.
- `probe-rs reset` and even `probe-rs download --reset` do **not** recover it —
  the reset is a reset-halt and the core is never resumed. Once a halt is left
  behind, probe-rs's chip auto-detection stops working too (*"The connected chip
  could not automatically be determined"*).
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
— there is **no `nRESET` line**. OpenOCD therefore resets the RP2350 through SWD
(the chip's ROM/debug reset), so `--connect-under-reset` is neither used nor
needed. `make flash-swd` also recovers a board whose previous flash left the core
halted or hard-faulted, with no button presses.

## Watching serial output

With the debug probe attached there are two USB serial devices: the badge's own
USB-CDC (the program's `fmt.Printf` output) and the debug probe's UART bridge.
`make monitor` selects the badge via `-target`; if that ever picks wrong, pass
`PORT=/dev/cu.usbmodemXXXX`.

The badge's USB VID:PID (`2e8a:000a`) is shared with other RP2350 boards, so
without `-target` TinyGo sees every USB serial port and refuses to guess:

```
multiple serial ports available - use -port flag, available ports are
/dev/cu.usbmodem11402, /dev/cu.usbmodem2101
```

You can always bypass TinyGo's monitor and read the port directly:

```sh
cat /dev/cu.usbmodemXXXX
```

USB-CDC logs each lifecycle transition (`launch: …`, `exit: start+select`,
`recovered update panic: …`), but only once a serial reader is attached — the
TinyGo USB-CDC drops writes until the host asserts DTR, so boot prints usually
appear only if `make monitor` was already running.

## Probe firmware

OpenOCD prints the probe firmware as `CMSIS-DAP: FW Version = …` (2.0.0 on the
probe used here, which works). Update from
<https://github.com/raspberrypi/debugprobe/releases> if needed.

## BusyBox-style CI notes

`.github/workflows/ci.yml` runs on push and pull request:

- **build** — installs TinyGo and runs `make build` + `make size`, uploading the
  `.uf2`/`.elf` artifacts. `tinygo build` is the real compile gate, since the
  `machine` package cannot be resolved by a stock Go toolchain.
- **format** — `gofmt -l` must be empty. (Generic Go linters such as
  golangci-lint are not used: they cannot type-check any file that imports
  `machine`.)
- **scan** — Trivy filesystem scan (CRITICAL/HIGH) and a gitleaks secret scan.
