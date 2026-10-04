TINYGOROOT := $(shell tinygo env TINYGOROOT)
TARGET     := targets/sycl-badge-v2.json
BOARD_SRC  := targets/board_sycl_badge_v2.go
BOARD_DST  := $(TINYGOROOT)/src/machine/board_sycl_badge_v2.go
UF2_VOLUME := /Volumes/RP2350

# SWD programming uses OpenOCD, not probe-rs.
#
# The badge is an RP2350B and the Raspberry Pi Debug Probe speaks CMSIS-DAP, so
# OpenOCD needs target/rp2350.cfg. That target only exists in OpenOCD git
# (>= the 0.12.0+dev HEAD) or the Raspberry Pi fork; the 0.12.0 release has
# only rp2040. See the README for install instructions.
OPENOCD       ?= openocd
OCD_INTERFACE ?= interface/cmsis-dap.cfg
OCD_TARGET    ?= target/rp2350.cfg
OCD_SPEED     ?= 5000

OCD := $(OPENOCD) -f $(OCD_INTERFACE) -f $(OCD_TARGET) -c "adapter speed $(OCD_SPEED)"

# Override to pick a specific port, e.g. `make monitor PORT=/dev/cu.usbmodem2101`
PORT       ?=

# Baked card lightshow assets. The source card images (assets/cards/) and the
# generated Go are both gitignored, so this is rebuilt on every build. Because
# imagery is gitignored, a worktree never owns a copy: tools/cards_dir.sh
# resolves ONE shared library for every worktree -- $SYCL_CARDS_DIR if set,
# else the main worktree's assets/cards, else ./assets/cards. With no
# manifest/sources present (e.g. a fresh clone or CI) it falls back to the
# original synthetic sample, so the firmware still compiles.
CARDS_DIR     := $(strip $(shell sh tools/cards_dir.sh 2>/dev/null))
ifeq ($(CARDS_DIR),)
CARDS_DIR     := assets/cards
endif
CARD_MANIFEST ?= $(CARDS_DIR)/manifest.json
CARD_OUT      ?= cartridge/cards_data.go

.PHONY: all build install-board uninstall-board flash flash-swd run-swd monitor clean size check-openocd sim sim-ui test card-data card-sample cards-dir cards-link cad-ref cad-check cad-build

all: build

# TinyGo resolves board definitions from $TINYGOROOT/src/machine, so the
# board file has to be synced into the toolchain before the build.
install-board:
	@cp "$(BOARD_SRC)" "$(BOARD_DST)"
	@echo "synced $(BOARD_SRC) -> $(BOARD_DST)"

uninstall-board:
	@rm -f "$(BOARD_DST)"
	@echo "removed $(BOARD_DST)"

card-data:
	@if [ -f "$(CARD_MANIFEST)" ]; then \
		python3 tools/make_card.py --manifest "$(CARD_MANIFEST)" -o "$(CARD_OUT)"; \
	else \
		echo "card-data: no $(CARD_MANIFEST); using synthetic sample"; \
		python3 tools/make_card.py --sample --name VOLTLET --types Electric -o "$(CARD_OUT)"; \
	fi

build: install-board card-data
	tinygo build -target=$(TARGET) -o hello.uf2 .
	tinygo build -target=$(TARGET) -o hello.elf .
	@ls -la hello.uf2

size: install-board card-data
	tinygo build -target=$(TARGET) -size=short -o /dev/null .

# Host-side build of the cartridge runtime (no machine package involved):
# runs the scripted menu/plasma/panic scenario and writes PNG frames.
SIM_OUT ?= sim-out
sim: card-data
	go run ./cmd/sim -out $(SIM_OUT)

# Host-side interactive sim: runs the same runtime at ~60 fps and serves the
# panel and controls to a browser window, so carts can be driven by hand with a
# keyboard or the on-screen buttons (no hardware). The sim loads cards from
# SIMUI_CARDS at startup using the same generator as the firmware build, so
# local card edits show up on relaunch without a rebuild; set SIMUI_CARDS= to
# force the baked cards. Extra flags via SIMUI_FLAGS, e.g.
# `make sim-ui SIMUI_FLAGS="-addr 127.0.0.1:8423 -open=false"`.
SIMUI_CARDS ?= $(CARDS_DIR)
SIMUI_FLAGS ?=
sim-ui: card-data
	go run ./cmd/simui -cards "$(SIMUI_CARDS)" $(SIMUI_FLAGS)

# Print the shared card directory all worktrees resolve to (see cards_dir.sh).
cards-dir:
	@echo "$(CARDS_DIR)"

# Link this worktree's assets/cards to the shared library, for tools that expect
# the documented path (the Makefile and cmd/simui resolve it directly anyway).
cards-link:
	@if [ "$(CARDS_DIR)" = "assets/cards" ]; then \
		echo "cards-link: already local ($(CARDS_DIR))"; exit 0; \
	fi; \
	if [ -e assets/cards ] || [ -L assets/cards ]; then \
		echo "cards-link: assets/cards already exists"; exit 1; \
	fi; \
	mkdir -p assets; \
	ln -s "$(abspath $(CARDS_DIR))" assets/cards; \
	echo "linked assets/cards -> $(abspath $(CARDS_DIR))"

# Host-side unit tests for the cartridge runtime.
test: card-data
	go test ./cartridge/...

# Regenerate the baked assets from the local manifest (see card-data).
card-sample:
	python3 tools/make_card.py --sample --name VOLTLET --types Electric \
		--rarity UNCOMMON -o "$(CARD_OUT)"

# Mass-storage load: the badge mounts as RP2350 when held in BOOTSEL mode
# (hold RESET + BOOT_SEL, release RESET, release BOOT_SEL).
flash: build
	@test -d "$(UF2_VOLUME)" || { echo "error: $(UF2_VOLUME) not mounted - put the badge in BOOTSEL mode"; exit 1; }
	cp hello.uf2 $(UF2_VOLUME)/
	@echo "flashed; badge should reboot and start printing"

# SWD load via a Raspberry Pi Debug Probe (CMSIS-DAP) and OpenOCD.
#
# `program ... verify reset exit` flashes, verifies, resets, and *runs* the core
# before OpenOCD exits - it is self-contained and does not leave the core
# halted. This is the reliable replacement for `probe-rs download`, which left
# the RP2350 core halted with no way to resume it (see README).
flash-swd: build check-openocd
	$(OCD) -c "program hello.elf verify reset exit"

# Kept as an alias so existing muscle memory / docs keep working. Previously
# this was `probe-rs run`, which flashed and reset but then blocked forever
# waiting for RTT output. The OpenOCD command above flashes and runs without
# holding the terminal.
run-swd: flash-swd

check-openocd:
	@command -v $(OPENOCD) >/dev/null 2>&1 || { \
		echo "error: $(OPENOCD) not found."; \
		echo "  install an RP2350-capable OpenOCD, e.g. 'brew install open-ocd --HEAD'"; \
		echo "  (Homebrew's stable 0.12.0 does not ship target/rp2350.cfg)"; \
		exit 1; \
	}

# The badge shares its USB VID:PID (2e8a:000a) with other RP2350 boards, and
# the debug probe adds a second USB serial port. Pass -target so TinyGo knows
# this board's VID:PID, and -port to disambiguate if needed.
monitor:
	tinygo monitor -target=$(TARGET) $(if $(PORT),-port=$(PORT),)

clean: uninstall-board
	rm -f hello.uf2 hello.elf
	rm -f cad/export/*.stl cad/export/*.3mf cad/export/*.f3d

# --- Card holder CAD ---------------------------------------------------------
#
# The tracked source of truth is cad/parameters.json (every dimension) plus
# cad/fusion/build.py (the geometry), never the .f3d. Reference geometry is
# derived from the hardware repo's KiCad file as text, so it needs no Fusion:
#
#   make cad-ref     regenerate cad/reference/board.json from the .kicad_pcb
#   make cad-check   ... and assert it is the board the plan describes
#
# Building the model does need Fusion, and Fusion has no local documents: the
# design lives in Autodesk's cloud and can only be exported as a snapshot. So
# the document is scratch state that cad/fusion/build.py rebuilds from the
# tracked text; cad/fusion/target.json names it. `make cad-build` only
# syntax-checks, because adsk.* exists nowhere else.
#
# Override the hardware checkout with: make cad-ref CAD_BOARD=/path/to.kicad_pcb
CAD_BOARD ?=
CAD_BOARD_ARG = $(if $(CAD_BOARD),--board "$(CAD_BOARD)",)

cad-ref:
	python3 cad/reference/kicad_extract.py $(CAD_BOARD_ARG)

cad-check:
	python3 cad/reference/kicad_extract.py --check $(CAD_BOARD_ARG)

cad-build:
	python3 -m py_compile cad/fusion/build.py
	python3 cad/fusion/build.py --check
	@echo "cad/fusion/build.py is valid; run it inside Fusion to rebuild the model"

