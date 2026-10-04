# Architecture: runtime, cores, memory, and failure

This page is the deep version of the "How it works" and "Cartridge runtime"
sections in the [README](../README.md). Read those first; come here when you
want to know exactly what the firmware is doing — which core runs what, when
the garbage collector runs, and what happens when things go wrong.

The short version of the design: **cartridges are Go values compiled into one
firmware, not separately loaded binaries.** The runtime swaps them, and the
whole hardware-free half lives in `cartridge/` so it can be unit-tested and
simulated on the host.

## Layers

```
main.go              package main: hardware side only
display.go           ST7735S panel driver (SPI0) + backlight PWM
hw.go, env_hw.go     adapt the panel and buttons to cartridge.Display/Env
env_demo.go          -tags=cartdemo scripted buttons (bring-up self-test)
cartridge/           hardware-free runtime + carts (imports no machine)
  cartridge.go       Cartridge, Factory, Platform, Runner
  screen.go          Width/Height, Buttons, RGB565, Display/Env
  plasma.go          PLASMA
  zeroman*.go        ZEROMAN
  pokecard.go        CARD SHOW
  panictest.go       PANIC TEST
cmd/sim, cmd/simui   host renderer and interactive browser simulator
tools/*.py           build-time asset generators
```

`main.go` is deliberately tiny: initialise the display, declare a list of
`Factory` values, hand them to a `Runner`, boot into CARD SHOW, and `Run()`
forever. Everything a cartridge can touch arrives through two interfaces instead
of hardware:

- `Display` — `Present(frame []uint16)` and `SetBacklight(level uint8)`.
- `Env` — `Buttons() Buttons`, `Millis() uint32`, `Sleep(ms uint32)`.

The host simulator substitutes its own `Display`/`Env` and therefore reuses the
identical menu, lifecycle, and carts.

## The two cores

Both cores run the **same TinyGo runtime**; nothing in this repository mentions
cores.

- The target (`targets/sycl-badge-v2.json`) inherits `rp2350b` → `rp2350` →
  `cortex-m33`. `rp2350.json` sets `"scheduler": "tasks"` (TinyGo's cooperative,
  round-robin task scheduler) and `__num_stacks=2`, so the linkerscript reserves
  a stack for each core. Our target raises `default-stack-size` to **8192**
  (from the Cortex-M default of 2048); TinyGo's `automatic-stack-size` may grow a
  goroutine's stack beyond that.
- In the runtime's `run()`, TinyGo runs `initRand`, `initHeap`, then launches one
  goroutine that runs all package initializers, **starts core 1**, sets
  `secondaryCoresStarted`, and calls `main.main`. `main.main` is itself a
  goroutine; the scheduler then loops.
- `startSecondaryCores` wakes core 1 through the RP SIO hardware FIFO start
  sequence. Core 1 enters the same scheduler but is given **no tasks of its
  own**. So all of our work — display init, the menu, carts, SPI — runs on
  **core 0**.
- `hasParallelism` is `false` for this scheduler. The second core exists for one
  job: the GC's stop-the-world phase. Core 1 enables the shared
  `SIO_IRQ_FIFO`; when core 0 collects, it pauses core 1, scans both stacks and
  the globals, and resumes it. Frame work stays single-core.

## Runtime state machine

`Runner.Run` is `for { Step() }`. Each `Step` samples the buttons and the clock,
resets the backlight to full, dispatches to `stepMenu`/`stepStart`/`stepRun`,
presents the frame, sets the backlight, then sleeps out the rest of the 16 ms
budget (~60 fps). Pacing goes through `Env.Sleep`, so the scripted host sim can
make it a fake-clock no-op.

- **`stMenu`** — up/down move the highlight, **A** constructs the cart with its
  factory (`lib[sel].New()`), logs `launch:`, and transitions.
- **`stStart`** — clears the frame, runs `Start` once inside `recover`; if it
  survives, arms the exit chord and falls straight into the run state for the
  first frame.
- **`stRun`** — checks the Start+Select chord, then calls `Update` inside
  `recover`.

The exit chord is armed only after it has been released, so holding
Start+Select across a launch does not bounce straight back to the menu.

A cart owns the frame for the duration of `Update`: it receives the shared
backbuffer via `p.Frame()` and must leave a full frame behind. Fresh state is a
consequence of construction: the runner stores factories, not instances, so
every launch is a `New()` on a zero-valued cart plus one `Start`. That replaces
the Zig reference firmware's BSS wipe between carts.

## What happens on a panic

`safeStart` and `safeUpdate` call the cart inside a deferred `recover`. A Go
panic is captured (`panicText`), the cart is dropped, the runner returns to the
menu, and the menu is redrawn with a `PANIC:` line; `recovered update panic: …`
is logged over USB-CDC. It does **not** re-initialise the panel — a recovered
cart panic is just a jump back into the runner loop, so the display keeps
working.

A panic **outside** a cart — in the menu, package init, or any other goroutine —
has no `recover`. TinyGo prints `panic: <value>` and calls `abort()`. On
Cortex-M `abort()` is:

```go
for { arm.Asm("wfi") }
```

a locked-up loop, dead until reset. A hard fault instead reaches
`HardFault_Handler`, which decodes the fault-cause bits (stack overflow, access
violation, divide by zero, …), prints `fatal error: …`, and aborts the same way.
The several `panic(...)` calls in `lcdInit` fall into this category: they run
before the recover loop exists, so a display-init failure is fatal by design.

## When GC runs

TinyGo uses a conservative, block-based mark/sweep collector (4-pointer blocks).
It is **not** on a timer and **not** on an allocation threshold. It runs only
inside `alloc()`: when the allocator cannot find a free range, it calls
`runGC()`, retries, and — if the freed bytes are under a third of the heap —
tries `growHeap()`. On baremetal `growHeap()` always returns false, so the heap
is fixed (everything in SRAM after `.bss`), and the path ends in
`runtimeFatal("out of memory")` if collection did not free enough.

The scheduler also offers an *idle* collection when it has no runnable task, but
our `Step` loop is always runnable, so in practice our GC is allocation-driven.

The production carts never call the GC directly. The HEAP teaching cart
(`cartridge/heap.go`, see [go-demos.md](go-demos.md)) is the one place that
calls `runtime.ReadMemStats` (to draw the heap bar) and `runtime.GC` (the
explicit collect), and it deliberately drives the allocator into
`runtimeFatal("out of memory")` to show that the fatal path is real.

## Do we have goroutines?

The production carts do not spawn goroutines, and on the badge `main` is the
only long-lived task. The two host-only `go` statements are `go runner.Run()` in
`cmd/simui/main.go` and the CFB serpentine in `cmd/sim`. The **TASKS teaching
cart** (`cartridge/tasks.go`) does spawn four worker goroutines, entirely for
show and tell; they self-terminate when the cart stops being updated (a
heartbeat the frame loop bumps goes stale) so they do not leak across
relaunches. See [go-demos.md](go-demos.md) for what they demonstrate.

The important invariant holds: the second core hosts no application task.
TinyGo's `tasks` scheduler is cooperative with `hasParallelism == false` and
`runtime.NumCPU() == 1`; core 1 runs the same scheduler but is given no work, and
exists only for the GC's stop-the-world phase. (`time.Sleep` is TinyGo's
cooperative `sleep`: it parks the current task on a sleep queue and yields to
the scheduler.)

## Memory budget and out-of-memory behaviour

The heap is a fixed SRAM region. Exceeding it triggers the GC path above; if a
collection cannot free enough and the heap cannot grow,
`runtimeFatal("out of memory")` prints `fatal error: out of memory` and aborts —
again a `wfi` lock-up, **not** a recoverable panic. Because that is fatal, the
runtime is built to be allocation-stable:

- One `Width*Height` RGB565 backbuffer (160×128×2 = **40 KB**) is allocated once
  in `NewRunner` and reused for every frame and every cart.
- Carts render in place into `Frame()`; there is essentially no steady-state
  allocation.
- Fixed-size extents: `Plasma.field` is `[Width*Height]uint8`; `CardShow` uses
  array tables (`sin`, `ramp`, `hueM`) and only allocates in `Start`
  (`buildPlasmaField`, `cals`, `spks`). `buildPlasmaField` is a single 20 KB
  transient that is immediately copied into the cart's own array.
- Baked card art travels as immutable Go strings, so it stays in flash `rodata`
  rather than being copied into RAM at boot.
- Per launch, a factory allocates one fresh cart struct (e.g. `Plasma` carries a
  20 KB field plus a 512-byte hue table), which is churned and eventually
  collected. Relaunching cycles allocations, letting the GC reclaim the previous
  instance.

If a future cart allocates per frame, that is the most likely source of a
runaway heap. The mitigation is the same invariant the existing carts follow:
allocate fixed extents once in `Start`, render in place. The HEAP cart's leak
mode is the deliberate exception, and it is the demo: watching the collector
chase per-frame allocation is the point.

The teaching carts also relax the allocation-free rule for `Start`: the TASKS
cart spawns four goroutines and the HEAP cart has a channel and retained slices.
Both are isolated to their own carts and bounded, and `make size` is the check
that they did not move the heap floor enough to matter.

## Why panic recovery is same-core and allocation-free

Recovery uses a deferred `recover()` inside the same call stack as `Update`, so
it costs nothing until something actually panics and needs no second goroutine
or context switch. Presenting is a direct function call (`lcdPresent`) from the
same loop, not a queue or an interrupt, which keeps frame ordering and the
one-backbuffer invariant trivial to reason about.
