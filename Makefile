TINYGOROOT := $(shell tinygo env TINYGOROOT)
TARGET     := targets/sycl-badge-v2.json
BOARD_SRC  := targets/board_sycl_badge_v2.go
BOARD_DST  := $(TINYGOROOT)/src/machine/board_sycl_badge_v2.go
UF2_VOLUME := /Volumes/RP2350

.PHONY: all build install-board uninstall-board flash monitor clean size

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

monitor:
	tinygo monitor

clean: uninstall-board
	rm -f hello.uf2 hello.elf
