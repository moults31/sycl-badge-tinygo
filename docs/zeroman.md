# ZEROMAN: the platformer port

`cartridge/zeroman*.go` is a port of `showcase/carts/zeroman` at 160×128. This
page records what that port entails. The [README](../README.md) only needs to
say it is a playable platformer cart.

## What was ported

The reference's package-level state — `GameData`, the player, enemies, room
transition, the text layer, RNG — lives on the cart struct and is rebuilt in
`Start` by the runner's fresh-construction lifecycle (see
[architecture.md](architecture.md)). Tiles and sprites are generated RGB565 with
packed palette indices, and the stage is parsed from the reference's
`needleman.zig`. Input comes from the platform's button snapshot; the cart does
not touch timers, SPI, or `machine`.

## Generated data

Both generated files are produced by `go generate`-style tools and committed
(unlike the card imagery, they are original/derived and small enough to ship):

- `cartridge/zeroman_gfx.go` — RGB565 palettes + packed sprite/tile indices
  (`tools/make_zeroman_gfx.py`).
- `cartridge/zeroman_stage.go` — the parsed stage data
  (`tools/make_zeroman_stage.py`).

The reference's Tracy zones are dropped, since TinyGo has no profiler of that
kind.

## Playing

Press a direction or **A** to leave the title, then move with the joystick and
jump (**A**) / shoot (**B**). Hold **Start+Select** to return to the menu.

## Why it is portable at all

ZEROMAN is the strongest evidence for the cartridge design: a real, stateful
game from the reference firmware becomes a Go struct plus two methods, with no
`machine` dependency, so `make test` and `make sim` cover it exactly as they
cover the menu. See [architecture.md](architecture.md) for the contract it
implements.
