# sycl-badge-tinygo

A minimal TinyGo "hello world" for the **SYCL Badge V2** — an RP2354B board
(48-GPIO RP2350B die, 2 MB in-package flash) running the SYCL 2026 production
(revision 2) hardware.

It blinks the board's user LED (GPIO14) and prints a counter over USB-CDC.

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
- `main.go` — the program.

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

On macOS the port looks like `/dev/cu.usbmodem*`; `make monitor` finds it, or:

```sh
cat /dev/cu.usbmodemXXXX
```

## Flashing over SWD (optional)

A Raspberry Pi Debug Probe on the badge's SWD port can flash over the debug
interface instead of UF2:

```sh
probe-rs download --chip RP235x hello.elf
```

**Note:** probe-rs 0.32 requires debug-probe firmware ≥ 2.2.0. Older probes
fail with *"firmware on the probe is outdated"*. Update the probe firmware from
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
