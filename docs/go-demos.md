# Go demonstrations: TASKS and HEAP

CARD SHOW is the app's hero. Alongside it, two menu entries exist purely for
show and tell: they make some Go/TinyGo runtime behaviour visible on the panel.
They are demos, not production code — live numbers, deliberately fatal paths,
and a reset to recover are all fine here.

| Cart | What it shows |
| --- | --- |
| **TASKS** | Goroutines on a cooperative, single-application-core scheduler. |
| **HEAP** | The conservative block GC, and what happens when the heap runs out. |

Both live in `cartridge/` (hardware-free), so they run in `make sim-ui` as well
as on the badge. `cartridge/platform_tinygo.go` sets `onBaremetal`, which lets
them keep the genuinely fatal demonstrations real on hardware while capping
themselves on the host.

## TASKS — goroutines on an embedded core

`cartridge/tasks.go`.

Four worker goroutines each do a small unit of work and report it over a
channel; the cart's own `Update` is the fifth participant ("MAIN"). The panel
shows each worker's reported count and a rolling strip of the order the reports
arrived, so the cooperative scheduler's interleaving is visible.

- **CPU 1** at the top is literal: `runtime.NumCPU()` returns 1 here. The
  RP2350 has two cores, but TinyGo's `tasks` scheduler has
  `hasParallelism == false`; core 1 runs the same scheduler with no tasks and
  waits to be paused for garbage collection.
- **A** toggles *greedy* on worker G1. G1 busy-loops for a fixed wall-clock
  budget with no blocking call. Because the scheduler is cooperative and not
  preemptive, **every** other goroutine — including the frame loop — is starved
  until G1 reaches the end of its budget and yields. The stall counter and the
  white trace mark record it; the other lanes then catch up, because `time.Sleep`
  is what lets the scheduler rotate.
- **B, then B again** (the second press confirms) spawns a goroutine that
  panics. The runner's `recover` in `safeUpdate` only unwinds the panicking
  goroutine's own stack, so a panic on a fresh goroutine is not caught: on the
  badge it reaches `abort()` (see
  [architecture.md](architecture.md#what-happens-on-a-panic)) and the chip locks
  up until reset. That is the deliberate contrast with **PANIC TEST**, whose
  panic happens inline in `Update` and *is* recovered.

Workers end themselves when the cart stops being updated: each keeps a
heartbeat the frame loop bumps every frame, and returns once it has been
unchanged for a while. That keeps the `Cartridge` contract at `Start`/`Update`
(no teardown hook) without leaking goroutines and their stacks across
relaunches.

## HEAP — the GC, and running out of memory

`cartridge/heap.go`.

The bar is live heap (`HeapInuse`) over usable heap (`HeapSys`), sampled with
`runtime.ReadMemStats` — the one place in the app that reads memory stats. The
counters are `NumGC`, live objects, and cumulative allocation, and the
`GC` indicator flashes when a collection happens.

- **A** allocates and retains a chunk; the live bar grows, and you can watch the
  collector reclaim only what it can. **B** drops the retained chunks and calls
  `runtime.GC()`; the bar drops and `NumGC` ticks.
- **Stick click** toggles a per-frame leak, which the collector chases without
  being able to win.
- **Hold A+B** (there is an on-screen arming bar) runs the headline demo: keep
  allocating and retaining until the allocator cannot find a free range, the
  collector cannot free enough, and `growHeap()` — a hard `false` on baremetal —
  fails. That lands in `runtimeFatal("out of memory")`, which prints
  `fatal error: out of memory` and calls `abort()`. It is **not** a Go panic and
  **not** recoverable by the runner; the only way back is reset, after which the
  badge boots normally into CARD SHOW.

On the host build the exhaustion demo stops at a fixed ceiling (`heapOOMCap`)
and reports "HOST CAPPED SAFELY", so a curious press in `make sim-ui` cannot
kill the simulator process. The host's Go heap also grows, so the host build
does not reproduce the real OOM; the real thing is a hardware lesson.

## Running them

```sh
make sim-ui    # drive both carts with the keyboard or the on-screen buttons
make test      # cartridge/tasks_test.go and cartridge/heap_test.go
make size      # confirm the extra carts did not blow the heap budget
```

The scripted `make sim` deliberately does not drive the nondeterministic modes;
it only exercises the menu and the other carts.

## Going deeper

[go-demos-in-depth.md](go-demos-in-depth.md) is the companion page: a guided
show-and-tell script for both carts, how each forced failure shows up in a real
program and how to defend against it, and a comparison of the scheduling,
recovery, and memory models against the Zig reference firmware.
