TINYGOROOT := $(shell tinygo env TINYGOROOT)
TARGET     := targets/sycl-badge-v2.json
BOARD_SRC  := targets/board_sycl_badge_v2.go
BOARD_DST  := $(TINYGOROOT)/src/machine/board_sycl_badge_v2.go
UF2_VOLUME := /Volumes/RP2350
PROBE_CHIP := RP235x

.PHONY: all build install-board uninstall-board flash flash-swd run-swd monitor clean size

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

# Mass-storage load: the badge mounts as RP2350 when held in BOOTSEL mode
# (hold RESET + BOOT_SEL, release RESET, release BOOT_SEL).
flash: build
	@test -d "$(UF2_VOLUME)" || { echo "error: $(UF2_VOLUME) not mounted - put the badge in BOOTSEL mode"; exit 1; }
	cp hello.uf2 $(UF2_VOLUME)/
	@echo "flashed; badge should reboot and start printing"

# SWD load via a debug probe (e.g. Raspberry Pi Debug Probe).
# NOTE: `probe-rs download` writes flash but leaves the core halted, so the
# program does not start. Use `make run-swd` to flash *and* run.
flash-swd: build
	probe-rs download --chip $(PROBE_CHIP) hello.elf

# Flash, reset, and run over SWD. Holds the terminal (RTT/log stream), so run
# it in its own session; `make monitor` still works in another terminal.
run-swd: build
	probe-rs run --chip $(PROBE_CHIP) hello.elf

monitor:
	tinygo monitor

clean: uninstall-board
	rm -f hello.uf2 hello.elf
