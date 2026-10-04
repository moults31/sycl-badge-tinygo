# Documentation

The [README](../README.md) is the front door: what the project is, how to build
and flash it, and how the cartridge runtime works. These pages go deeper, for
when a specific question comes up.

| Page | Read it when you want to… |
| --- | --- |
| [architecture.md](architecture.md) | Understand the runtime: the two cores, runtime vs. app, when GC runs, what a panic does, the memory budget. |
| [display.md](display.md) | Work on the panel or backlight, or understand the `MADCTL` orientation and the two hardware post-mortems. |
| [cards.md](cards.md) | Understand or extend CARD SHOW: effects, colour modes, assets, the manifest, calibration. |
| [zeroman.md](zeroman.md) | Understand the platformer port and its generated data. |
| [go-demos.md](go-demos.md) | Understand the TASKS and HEAP show-and-tell carts: goroutines and the GC on the badge. |
| [go-demos-in-depth.md](go-demos-in-depth.md) | Run the show-and-tell, then dig into the real-world failure modes, defenses, and the Zig reference comparison. |
| [toolchain.md](toolchain.md) | Build the custom target, use the shared card library, bring up without buttons, flash over SWD, or watch serial. |
| [pokemon-card-holder-plan.md](pokemon-card-holder-plan.md) | Follow the mechanical (CAD) design of the physical card holder. |
