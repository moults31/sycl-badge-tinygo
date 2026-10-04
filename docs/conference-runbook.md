# Conference runbook: rebuilding and re-flashing the badge on a fresh MacBook

This is the "something broke at the conference" plan. The other MacBook is
identical hardware but has none of our tooling; this page gets it from nothing
to **editing the source, rebuilding, and flashing over SWD**.

The whole path is scripted in one place, `scripts/conference-setup.sh`, so the
checklist is short. The rest of this page explains each step, what can go wrong,
and the fastest recovery.

- **Assumed hardware:** Apple-silicon MacBook (arm64), the Raspberry Pi Debug
  Probe, the badge, USB-C cables, and the SWD cable.
- **Assumed goal:** be able to `git pull`, edit a cart, `make flash-swd`, and see
  the fix on the badge — *not* just re-flash an unchanged build.
- **Time budget:** the only slow step is OpenOCD's `--HEAD` build (a few
  minutes). Everything else is minutes.

## TL;DR

On the new MacBook, with the repo cloned and the probe + badge plugged in:

```sh
# one-time toolchain bring-up
brew install go python@3.12
python3 -m pip install --user pillow
brew install open-ocd --HEAD          # slow: source build; ships target/rp2350.cfg
brew install tinygo                   # or the tarball if the bottle refuses

# sanity-check the toolchain without hardware
make test
make build

# with the SWD probe on the badge
make flash-swd
```

If `make build` and `make flash-swd` both run clean, you are ready.

## What the build actually needs

There are four external things; nothing else in the repo is machine-specific
(the card imagery is gitignored and the build falls back to a synthetic sample,
so a fresh clone compiles).

| Need | Why | Install |
| --- | --- | --- |
| Go 1.25–1.27 on `PATH` | TinyGo reuses the system Go toolchain (`GOROOT`); the `go.mod` says `go 1.26` | `brew install go` |
| TinyGo 0.42.0+ | Compiles the firmware; also used for `make size` | `brew tap tinygo-org/tools && brew install tinygo` |
| Python 3 + Pillow | `tools/make_card.py` imports PIL at module load and runs on **every** build (`make card-data`); without it the build stops | `python3 -m pip install --user pillow` |
| OpenOCD with `target/rp2350.cfg` | `make flash-swd` drives OpenOCD, not probe-rs | `brew install open-ocd --HEAD` |

Two details that bite on a fresh machine:

- **Homebrew's stable `open-ocd` (0.12.0) does not ship `rp2350.cfg`.** You must
  use `--HEAD` (or the raspberrypi fork). `make flash-swd` checks and fails
  early with a clear message if it is missing.
- **Pillow is not optional.** `make_card.py` imports PIL unconditionally, even
  for the built-in synthetic sample, so a missing Pillow stops `make build`. CI
  installs it for exactly this reason (`.github/workflows/ci.yml`).
- **Homebrew's TinyGo bottle can refuse to install on an older Xcode.** If it
  does, use the release tarball:
  <https://github.com/tinygo-org/tinygo/releases> (documented in the README).

## Step by step

### 1. Get the repo

```sh
git clone <repo> sycl-badge-tinygo
cd sycl-badge-tinygo
```

The remote is currently SSH (`git@github.com:...`). If the new MacBook has no
SSH key set up, either add one or switch that clone to HTTPS:

```sh
git remote set-url origin https://github.com/moults31/sycl-badge-tinygo.git
```

If you will push a conference fix, authenticate (GitHub CLI is easiest):
`brew install gh && gh auth login`.

### 2. Install the toolchain

Run the script (`scripts/conference-setup.sh`), or do it by hand:

```sh
brew install go python@3.12
python3 -m pip install --user pillow

# OpenOCD with the RP2350 target. This is the slow one (source build).
brew install open-ocd --HEAD

# TinyGo. If the bottle refuses because of Xcode, use the tarball instead.
brew tap tinygo-org/tools && brew install tinygo
```

Verify:

```sh
go version                 # 1.25–1.27
tinygo version             # 0.42.0 or newer
python3 -c 'import PIL; print(PIL.__version__)'
openocd --version          # any 0.12+; the --HEAD build is fine
```

### 3. Build without hardware (do this first)

```sh
make test     # host-side runtime tests; no hardware, fast
make build    # produces hello.uf2 and hello.elf
make size     # optional: flash/RAM usage
```

`make build` syncs `targets/board_sycl_badge_v2.go` into `$TINYGOROOT/src/machine`
first (that is normal and expected — see [toolchain.md](toolchain.md)). It also
regenerates `cartridge/cards_data.go` from the synthetic sample because there is
no local card library; that is also normal.

If these pass, your toolchain is sound and any later failure is hardware/cable,
not software.

### 4. Wire the badge and flash

1. Connect the Raspberry Pi Debug Probe to the badge's 3-pin SWD header
   (`SWCLK`, `SWDIO`, `GND` — there is no `nRESET` line; OpenOCD resets through
   SWD).
2. Connect the badge to the MacBook over USB (this also powers it and provides
   the USB-CDC serial port).
3. Plug the probe into the MacBook over USB.

```sh
make flash-swd
```

Expected tail:

```
Info : RP2350 rev 3, QSPI Flash win w25q16jv id = 0x1540ef size = 2048 KiB in 512 sectors
** Programming Finished **
** Verify Started **
** Verified OK **
** Resetting Target **
```

`flash-swd` flashes, **verifies**, resets, and releases the core running — no
BOOTSEL button dance, and it recovers a board left halted by a previous bad
flash. When it prints **Verified OK**, the new firmware is on the badge.

### 5. Confirm it booted (optional)

```sh
make monitor            # uses the target's VID:PID to pick the badge port
# or, if it picks the probe's UART instead:
make monitor PORT=/dev/cu.usbmodemXXXX
```

Remember the TinyGo USB-CDC quirk: boot prints only appear if a reader was
already attached, so a silent monitor after a fresh flash is normal. The panel
itself (CARD SHOW on boot) is the real confirmation.

## Flashing without any toolchain (quick fallback)

If the failure is **not** a source change — the badge is bricked, or you just
want the last known-good build back fast — skip the whole toolchain and use CI's
artifact. CI builds every push to `main` and uploads `hello.uf2` and `hello.elf`.

- Download the **`firmware`** artifact from the latest successful `main` run on
  GitHub (Actions → the run → Artifacts). Artifacts are kept for ~90 days.
- You still need OpenOCD to use the `.elf`, so `brew install open-ocd --HEAD` is
  unavoidable for SWD. Alternatively flash the `.uf2` over the mass-storage
  bootloader with no tools at all: put the badge in BOOTSEL
  (hold `RESET` + `BOOT_SEL`, release `RESET`, then `BOOT_SEL`), copy `hello.uf2`
  to the mounted `RP2350` volume, and it reboots.

This fallback is for *unchanged* firmware only. If you are patching a bug, you
need the full toolchain above.

## The conference fix loop (the actual reason for this page)

Once the toolchain is up, fixing a cart is the normal loop:

```sh
git pull
# edit cartridge/<something>.go
make test          # catch mistakes on the host first
make build         # or go straight to the next line
make flash-swd     # rebuilds and flashes in one step
```

`make flash-swd` depends on `build`, so it always reflects the current source.
For a faster inner loop you can flash the `.elf` directly without `make`:

```sh
tinygo build -target=targets/sycl-badge-v2.json -o hello.elf .
openocd -f interface/cmsis-dap.cfg -f target/rp2350.cfg \
  -c "adapter speed 5000" -c "program hello.elf verify reset exit"
```

The teaching carts (TASKS/HEAP) deliberately include fatal demos that lock the
chip until reset; if the badge "dies" during a demo, that is expected — reset it,
or just re-run `make flash-swd`, which recovers a halted core.

## Bring-up checklist

Copy this into a scratch note and tick it off:

- [ ] Repo cloned; remote reachable (`git remote -v`, `git fetch`).
- [ ] `go version` is 1.25–1.27.
- [ ] `tinygo version` is 0.42.0+.
- [ ] `python3 -c 'import PIL'` succeeds.
- [ ] `openocd` runs and `target/rp2350.cfg` exists
      (`ls "$(brew --prefix)/share/openocd/scripts/target/rp2350.cfg"`).
- [ ] `make test` passes (no hardware needed).
- [ ] `make build` produces `hello.elf` and `hello.uf2`.
- [ ] Debug Probe + badge wired and on USB.
- [ ] `make flash-swd` prints **Verified OK** and the badge shows CARD SHOW.
- [ ] (If you might push a fix) `gh auth status` is logged in.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `make build` stops in `card-data` / `ModuleNotFoundError: PIL` | `python3 -m pip install --user pillow` (Pillow is required by `tools/make_card.py`). |
| `make flash-swd` says `openocd not found` | `brew install open-ocd --HEAD`. |
| OpenOCD can't find `target/rp2350.cfg` | Stable 0.12.0 lacks it; reinstall `--HEAD`, or point `OCD_TARGET` at a config from the raspberrypi fork. |
| `tinygo: command not found` after install | Open a new shell (`brew` updates `PATH`), or add `$(brew --prefix)/bin`. |
| TinyGo bottle refuses to install | Use the release tarball (see README prerequisites). |
| OpenOCD can't see the probe | Reseat the probe USB and the 3-pin SWD cable; `openocd` should print `CMSIS-DAP` and an `SWD DPIDR`. Try a different USB port/hub (a direct port, not a dock). |
| `make monitor` picks the probe's UART | Pass the badge port explicitly: `make monitor PORT=/dev/cu.usbmodemXXXX`. |
| Badge dark after a demo | Expected for TASKS/HEAP fatal demos — reset, or re-run `make flash-swd`. |
| A push is rejected (no auth) | `gh auth login`, or switch the remote to HTTPS and use a token. |

## Notes on the Copilot-only MacBook

The build does not care about our OpenCode subscription: everything above is
ordinary `git` + `brew` + `tinygo` + `openocd`. If you want an AI assistant
while editing a fix, the corporate GitHub Copilot subscription is enough for
editing Go — but note it is **not** part of the build path, so its presence or
absence changes nothing about this runbook.
