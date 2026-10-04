#!/usr/bin/env bash
#
# conference-setup.sh -- bring a fresh Apple-silicon MacBook from nothing to
# "can build and SWD-flash the badge".
#
# See docs/conference-runbook.md for the narrative, the checklist, and the
# troubleshooting table. This script is the executable version of steps 2-5.
#
# It is idempotent: re-running skips what is already installed. It does NOT
# touch hardware; run `make flash-swd` yourself once the probe is connected.
#
# Usage:
#   scripts/conference-setup.sh              # install toolchain, then test+build
#   scripts/conference-setup.sh --no-build   # toolchain only
#
set -euo pipefail

DO_BUILD=1
for arg in "$@"; do
	case "$arg" in
	--no-build) DO_BUILD=0 ;;
	-h | --help)
		sed -n '2,12p' "$0"
		exit 0
		;;
	*)
		echo "unknown argument: $arg" >&2
		exit 2
		;;
	esac
done

info() { printf '\n==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

if ! have brew; then
	echo "error: Homebrew is required but not on PATH." >&2
	echo "  install it from https://brew.sh, then re-run this script." >&2
	exit 1
fi

# --- Go (TinyGo reuses the system Go toolchain) ------------------------------
info "Go"
if have go; then
	echo "  present: $(go version)"
else
	brew install go
fi

# --- Python 3 + Pillow (required by tools/make_card.py on every build) -------
info "Python 3 + Pillow"
if have python3; then
	echo "  present: $(python3 --version 2>&1)"
else
	echo "  installing python@3.12"
	brew install python@3.12
fi
if python3 -c 'import PIL' >/dev/null 2>&1; then
	echo "  Pillow present"
else
	echo "  installing Pillow (required by tools/make_card.py)"
	python3 -m pip install --user pillow
fi

# --- TinyGo ------------------------------------------------------------------
info "TinyGo"
if have tinygo; then
	echo "  present: $(tinygo version)"
else
	echo "  installing tinygo (bottle). If this refuses because of Xcode, use the"
	echo "  release tarball instead: https://github.com/tinygo-org/tinygo/releases"
	brew tap tinygo-org/tools
	brew install tinygo
fi

# --- OpenOCD with the RP2350 target ------------------------------------------
# Stable open-ocd 0.12.0 predates the RP2350 and has no target/rp2350.cfg, so a
# HEAD (source) build is required for `make flash-swd`.
info "OpenOCD (with target/rp2350.cfg)"
ocd_prefix="$(brew --prefix open-ocd 2>/dev/null || true)"
if have openocd && [ -n "$ocd_prefix" ] && [ -f "$ocd_prefix/share/openocd/scripts/target/rp2350.cfg" ]; then
	echo "  present: $(openocd --version 2>&1 | head -1)"
else
	echo "  installing open-ocd --HEAD (source build, a few minutes)"
	brew install open-ocd --HEAD || {
		warn "open-ocd --HEAD failed. Try the raspberrypi fork, or the OpenOCD"
		warn "bundled with the raspberrypi.pico-vscode extension."
	}
fi

# --- Verify versions ---------------------------------------------------------
info "Verifying toolchain"
if have go; then
	echo "  go:      $(go version | awk '{print $3}')"
else
	warn "go not found on PATH"
fi
if have tinygo; then
	echo "  tinygo:  $(tinygo version 2>&1 | awk '{print $3}')"
else
	warn "tinygo not found on PATH"
fi

# --- Build without hardware --------------------------------------------------
if [ "$DO_BUILD" -eq 1 ]; then
	info "Host tests (no hardware needed)"
	make test

	info "Firmware build"
	make build

	info "Done. Next: wire the Debug Probe + badge and run 'make flash-swd'."
else
	info "Toolchain ready. Re-run without --no-build, or run 'make test && make build'."
fi
