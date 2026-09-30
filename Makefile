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

.PHONY: all build install-board uninstall-board flash flash-swd run-swd monitor clean size check-openocd sim test card-sample

all: build

# TinyGo resolves board definitions from $TINYGOROOT/src/machine, so the
# board file has to be synced into the toolchain before the build.
install-board:
	@cp "$(BOARD_SRC)" "$(BOARD_DST)"
	@echo "synced $(BOARD_SRC) -> $(BOARD_DST)"

uninstall-board:
	@rm -f "$(BOARD_DST)"
	@echo "removed $(BOARD_DST)"

build: install-board
	tinygo build -target=$(TARGET) -o hello.uf2 .
	tinygo build -target=$(TARGET) -o hello.elf .
	@ls -la hello.uf2

size: install-board
	tinygo build -target=$(TARGET) -size=short -o /dev/null .

# Host-side build of the cartridge runtime (no machine package involved):
# runs the scripted menu/plasma/panic scenario and writes PNG frames.
SIM_OUT ?= sim-out
sim:
	go run ./cmd/sim -out $(SIM_OUT)

# Host-side unit tests for the cartridge runtime.
test:
	go test ./cartridge/...

# Regenerate the committed sample card asset (original synthetic art, no
# third-party imagery) from tools/make_card.py.
card-sample:
	python3 tools/make_card.py --sample --name VOLTLET --types Electric \
		--rarity UNCOMMON -o cartridge/card_data.go

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
