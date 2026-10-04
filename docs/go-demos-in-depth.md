# TASKS and HEAP: show-and-tell, and what the demos really mean

This page is the guided-tour companion to [go-demos.md](go-demos.md). That page
explains *what the two teaching carts are*; this one is for actually running
them, then unpacking the runtime behaviour they force — how the same failure can
happen in a real program, how you would defend against it, and how the whole
picture compares with the Zig reference firmware the port grew out of.

The carts are deliberately not production code. Live numbers, deliberate
stalls, and paths that lock the chip up until reset are the point.

## Part 1 — Running the show-and-tell

### Setup

```sh
make build      # build hello.uf2 / hello.elf
make flash      # mass-storage: copy to the RP2350 volume
make flash-swd  # SWD: OpenOCD + a Raspberry Pi Debug Probe
make sim-ui     # drive the same runtime in a browser, no hardware needed
```

After flashing, the badge boots into **CARD SHOW**. Hold **Start+Select** for
250 ms to reach the menu; **TASKS**, **HEAP**, and the other carts are all there.
Whatever happens below, the way back to a clean state is a **reset** — the badge
reboots into CARD SHOW with no side effects.

The exit chord is always **Start+Select** (250 ms). A and B are free inside the
carts.

### TASKS — a five-minute script

1. Launch **TASKS**. Four lanes (G0..G3) count up, each with a progress bar, and
   the **SCHED** strip at the bottom records the order reports arrived. Watch it
   for a few seconds: a clean cyan→green→pink→yellow cycle. That is the
   cooperative scheduler handing control round-robin, because every worker
   yields at its `time.Sleep`.
2. Note **CPU 1** at the top right. That is `runtime.NumCPU()` on the badge: one
   application core, even though the RP2350 has two.
3. Press **A**. Worker G1 goes *greedy*: it busy-loops without any blocking call.
   Every other lane — and the frame loop itself — freezes until G1's budget
   expires. The strip shows a run of green; the status line reads
   **STALL: NO PREEMPT**. When G1 finally yields, the other lanes catch up. The
   scheduler is cooperative, not preemptive: nothing takes the CPU away from
   G1, so G1 has to give it back.
4. Press **A** again to turn greedy off and watch it return to a clean rotation.
5. Press **B** once: the status line shows **B AGAIN = GO PANIC**. Press **B**
   again to confirm. On hardware the screen freezes and the chip stops; reset to
   recover. This is the contrast with **PANIC TEST**, whose panic happens inline
   in `Update` and is caught by the runner's `recover`.

### HEAP — a five-minute script

1. Launch **HEAP**. The bar is live heap over usable heap (`HeapInuse` /
   `HeapSys`), sampled with `runtime.ReadMemStats`. Below it: GC cycles, live
   objects, cumulative allocation, and bytes you are holding.
2. Press **A** a few times. The live bar and **HELD** grow. Note the **GC**
   counter does not necessarily move yet: the collector is not on a timer, so it
   runs when the allocator next needs free space, not when you allocate.
3. Press **B** to drop the retained chunks and collect explicitly. The bar falls
   and the GC counter ticks.
4. Press the **stick click** to toggle a per-frame leak. The bar climbs and the
   collector chases it; you are watching allocation-driven collection happen in
   front of you.
5. Now the headline: **hold A+B**. An arming bar fills; when it completes the
   status reads **!! OOM IN PROGRESS** and the cart keeps allocating retained
   chunks. On hardware, before long the heap cannot be satisfied, `growHeap()`
   returns false, and the runtime prints `fatal error: out of memory` and
   `abort()`s — the chip locks up in a `wfi` loop. Reset to recover.
   On the host sim the same sequence stops at a fixed ceiling and reports
   **HOST CAPPED SAFELY**, so a curious press cannot kill the simulator.

### What "reset to recover" means

Both repeated-fatal demos end in TinyGo's `abort()`, which on Cortex-M is a
locked `for { wfi }` loop. This firmware does not arm a watchdog, so the badge
stays dark and unresponsive until you press reset. After reset it boots normally
into CARD SHOW — nothing is persisted, so there is no corrupt state to clean up.
That is the safe part of an otherwise unsafe demo: the failure is total and
immediate, but it is not *sticky*.

For a real product the missing watchdog is the gap. TinyGo exposes the RP2350's
hardware watchdog (`machine.Watchdog`), and arming it with a timeout that the
main loop feeds would turn either fatal path into an automatic reboot instead of
a manual reset. That is the standard answer to "the process can die
unexpectedly": make the death recoverable at the system level rather than trying
to make it impossible at the language level.

## Part 2 — The practical ramifications

### 2.1 Cooperative scheduling and the no-yield stall

**What the demo forces.** G1 busy-loops and nothing else runs until it stops.

**How it happens for real.** Any goroutine that spins without hitting a blocking
point starves the rest of the program: `for { poll(); }` with no `Sleep`,
`Gosched`, channel, or mutex; a retry loop waiting on a hardware flag; a
`for !done {}` on a variable another goroutine is supposed to set; a
CRC/parse loop over a large buffer. On a cooperative scheduler, "another
goroutine will run in a bit" is only true if the current one yields. CPU-bound
work and tight polling loops are the usual culprits. A second common case is a
`select` with a `default:` that never blocks — it looks concurrent but never
gives the scheduler a chance.

**How to defend against it.** The rules of cooperative multitasking are simple
but easy to forget:

- **Yield explicitly in long loops.** `runtime.Gosched()` hands the CPU back at
  a safe point; a `time.Sleep(0)` or a channel handoff works too. On TinyGo,
  `Gosched` is exactly "push myself on the runqueue and pause".
- **Prefer blocking primitives over spinning.** A receive on a channel, a mutex,
  or a `time.Sleep` all park the goroutine, which is the scheduler's chance to
  rotate. Design for *events*, not *polling*.
- **Bound work per frame.** If a task must run long, break it into chunks and
  yield between chunks — a job queue drained one item per frame, which is
  exactly what the HEAP cart does with its per-frame allocation step.
- **Never assume preemption.** Neither Go-on-TinyGo nor the Zig runtime gives
  you interrupts as a scheduler. If the design needs a hard real-time bound, it
  needs a timer/interrupt path, not a goroutine.
- **Treat a busy loop as a bug to be found.** The TASKS cart's stall counter is
  a toy version of the real technique: instrument the yield points and alert
  when a worker has not yielded in too long.

**The framing that makes it click.** Concurrency (having many pending tasks) is
not parallelism (running many at once). TinyGo's `tasks` scheduler gives you
concurrency on one core with cooperative switches; the second RP2350 core is
reserved for the collector, not for your tasks. So a non-yielding goroutine does
not just get *less* CPU — it gets *all* of it, and everything else stops.

### 2.2 A panic in the wrong goroutine

**What the demo forces.** A panic on a fresh goroutine is not caught by a
`recover` that lives on the `Update` call stack, so the program dies.

**How it happens for real.** `recover` only unwinds the panicking goroutine's
own stack; it cannot see a panic on a sibling goroutine. Any code that spawns
`go func(){ ... }` and does not put its own `recover` inside that function has an
unrecoverable-on-the-parent failure. Realistic triggers: a bare
`go worker(ch)` that hits a nil map write or an index out of range; a panic in a
callback the LA spawned; a library that panics on bad input on its own goroutine.
The parent keeps running its normal loop right up until the runtime prints
`panic:` and aborts, so the failure looks like it came from nowhere.

**How to defend against it.** Go's own idiom is the boundary-with-recover:

```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            log("worker recovered:", r) // report, then exit or restart
        }
    }()
    worker()
}()
```

- **Put a `recover` inside every spawned goroutine** whose failure must not take
  the process down, and decide there whether to log-and-exit, restart the
  worker, or propagate through an error channel.
- **Do not panic across package boundaries** as a control-flow mechanism; return
  errors and let the caller decide.
- **Validate before spawning** — the goroutine boundary is a poor place to
  discover a nil map, a nil channel send, or a bad index.
- **Make the failure observable.** On a device with logs, the panic text is your
  only clue; this firmware's runner logs recovered panics over USB-CDC, but a
  goroutine panic never reaches that path, which is exactly the profile.
- **Know the abort policy.** When no recover is in scope TinyGo runs
  `panicStrategy()` and, in the default "print" strategy, prints and calls
  `abort()` (a `wfi` lockup on Cortex-M). A watchdog or a reset-on-fault policy
  is the system-level backstop.

### 2.3 Heap exhaustion and the fatal allocator path

**What the demo forces.** Retained, unbounded allocation eventually makes the
allocator unable to find a free range; `runGC` frees too little and `growHeap()`
is a hard `false` on baremetal, so the runtime calls
`runtimeFatal("out of memory")` and aborts. It is **not** a Go panic and **not**
recoverable.

**How it happens for real.** The TASKS/HEAP "leak" is a caricature of ordinary
mistakes: appending to a slice or map that is never trimmed; caching every input
in a long-lived structure; a log buffer that grows without a cap; retaining
sub-slices of a large buffer (which keeps the whole backing array alive); a
per-frame allocation in a loop that the collector cannot keep up with because
the program holds the previous frame's data. On a device the heap is a fixed
SRAM region, so there is no "plenty of RAM" to fall back on.

**How to defend against it.** This is the one place the codebase's normal rule
is a hard rule:

- **Allocate fixed extents once, reuse forever.** The app's core invariant: one
  40 KB backbuffer allocated once in `NewRunner`, carts render in place, no
  steady-state allocation. Plan the memory budget up front.
- **No allocation per frame or in hot loops.** Prefer arrays and reusable
  scratch buffers to `make`/`append`. The HEAP cart's leak mode is the
  anti-pattern.
- **Bound every cache and queue.** If a structure can grow with input, it needs
  a size cap and an eviction policy.
- **Watch the live set, not just the counter.** `ReadMemStats` gives
  `HeapInuse`/`HeapSys`; a rising floor that collection cannot lower is a leak,
  not churn. This is the number HEAP draws as the bar.
- **Treat OOM as fatal by design.** Because there is no recover, the mitigation
  is architectural: allocation-stable code plus, for a product, a watchdog that
  resets into a known-good state.
- **Separate "the collector is slow" from "the program is leaking".** The first
  is churn and is survivable; the second ends in `out of memory`. The demo's
  explicit `B` (drop + `runtime.GC`) is the diagnostic: if collecting by hand
  does not lower the bar, you are holding onto something.

### 2.4 Why the demos are gated, and why the arm step exists

Both fatal demos make you confirm (B twice, or hold A+B to fill a bar). That is
not just theatre: an accidental reset is a bad experience, and a demo that
people are afraid to run teaches nothing. The gating also mirrors a real
practice — dangerous operations behind an explicit, deliberate confirmation.

On the host build, `cartridge/platform_host.go` sets `onBaremetal = false` and
the carts cap themselves. The desktop Go runtime is preemptive, multithreaded,
and its heap grows, so the sim *cannot* reproduce the stall or the OOM; it shows
the interface and the numbers, and labels the fatal paths as simulated. The real
lesson is a hardware lesson. This split is itself the portability point: the
hardware-free `cartridge/` package runs both, and a two-constant build tag picks
the honest behaviour for each.

## Part 3 — How this compares with the Zig reference

The port moved the reference firmware from Zig to TinyGo. The runtime facts the
demos exercise are the clearest place to see what that move buys and what it
costs. This is a comparison of *models*, not a benchmark.

### 3.1 Scheduling and tasks

| | Zig reference architecture | TinyGo port |
| --- | --- | --- |
| Main loop | Explicit supervisor loop; drivers run to completion | `for { Step() }` in a goroutine; scheduler rotates cooperating tasks |
| Concurrency unit | None at the language level; state machines / explicit polling | Goroutines, channels, `select`, `time.Sleep`, mutexes |
| Preemption | None; the loop owns the core | None for app tasks (cooperative); core 1 reserved for GC |
| Yielding | Implicit: functions return to the loop | Explicit: block on a primitive, `Sleep`, or `Gosched` |
| Failure if a step overruns | Loop stalls until the step returns | Any goroutine can starve the others until it yields |

The Zig model makes the frame budget visible by construction: if a step is slow,
the whole loop is slow and you notice. The Go model is more expressive — you can
write a producer/consumer, a worker pool, a timeout — but it moves the yield
points into *your* code, and a missed one is the TASKS stall. Both architectures
are single-application-core here; neither gives true parallelism to app code.
Go's version is more comfortable to write and easier to get subtly wrong.

### 3.2 Failure, recovery, and process death

| | Zig | TinyGo |
| --- | --- | --- |
| Recoverable error path | Explicit error unions (`!T`) propagated by hand | `panic`/`recover`, plus error returns |
| In-cart failure | Depends on design; no language `recover` | `defer recover()` around `Start`/`Update` returns to the menu with a `PANIC:` line |
| Cross-task failure | N/A (no tasks) | A panic on a spawned goroutine is *not* caught by the parent's recover |
| Fatal/OOM path | Abort/hang; watchdog expected | `runtimeFatal` → `abort()` → `wfi` lockup; same watchdog conclusion |

Zig's error unions force every fallible call to be handled at the call site —
verbose, but nothing is silently deferred. Go's `panic`/`recover` is ergonomic
for the common case and is what makes the cartridge runtime's "a bad cart
returns to the menu" trick clean, but it is *stack-local*: the goroutine
boundary is a recovery boundary you must remember to install. The Zig reference
has no equivalent footgun because it has no goroutines; its analogous footgun is
an unrecovered `err` that propagates to `main`.

### 3.3 Memory and the collector

| | Zig | TinyGo |
| --- | --- | --- |
| Allocation | Explicit allocator, often arena/fixed buffers | `make`/`new`/`append`, heap-managed |
| Reclamation | Manual (or arena reset) — deterministic | Conservative mark/sweep, allocation-driven, no timer |
| OOM result | Whatever the allocator/design does; often a panic/hang | `runtimeFatal("out of memory")` → `abort()` → `wfi` |
| Failure mode to fear | Use-after-free, leak, fragmentation | Live-set growth the collector cannot reclaim; no `free`-time control |

This is the deepest difference. Zig gives you manual control and deterministic
reclamation — you can *prove* the memory budget — but you own every free and
every dangling pointer. TinyGo removes use-after-free (the collector won't free
reachable memory) and makes ordinary code allocation-heavy, at the cost of a
collector whose schedule you do not control and a fatal, unrecoverable OOM path.
The port's answer is the same as the reference's in spirit: keep the live set
fixed and small. The HEAP cart exists precisely because that rule is invisible
until it is violated.

### 3.4 Panics vs. abort, and debuggability

| | Zig | TinyGo |
| --- | --- | --- |
| Assertion failure | `panic`/`unreachable`, configurable handler | `panic()` → recover or `abort()` |
| Out-of-memory | Depends on allocator | `runtimeFatal` (never recoverable) |
| Finding the cause | Reference used Tracy zones; now no profiler | USB-CDC logs lifecycle; svd/SWD via OpenOCD |

The reference firmware's Tracy zones were dropped in the port (see
[zeroman.md](zeroman.md)) because TinyGo has no equivalent profiler. What remains
is the recover-path log over USB-CDC and the SWD/OpenOCD debug path described in
[toolchain.md](toolchain.md). In practice both firmwares are debugged the same
way: reproduce, then instrument or attach.

### 3.5 Pros and cons of the move, in one place

**TinyGo/Go pros**

- Goroutines and channels make concurrent, event-shaped code expressible without
  hand-rolled state machines.
- Same-source testability: `cartridge/` imports no `machine`, so `make test` and
  `make sim`/`make sim-ui` cover the runtime and every cart on the host.
- `defer recover()` gives a clean per-cart containment boundary (a bad cart
  returns to the menu) that is hard to get this ergonomically in Zig.
- Familiar language and tooling; one firmware holds all carts as Go values.

**TinyGo/Go cons**

- The cooperative scheduler makes yields *your* responsibility; a missed yield
  starves everything (the TASKS stall).
- Recovery is stack-local; a panic on a spawned goroutine is fatal (the TASKS
  panic).
- The GC's schedule is not yours; the live set can grow into an unrecoverable
  OOM (the HEAP OOM).
- No profiler equivalent to Tracy; larger binary and a runtime you do not fully
  hold in your head.

**Zig reference pros**

- Deterministic memory and explicit error handling; you can reason about the
  whole budget and every failure.
- No goroutine surprises: one loop, one core, no starvation from a sibling task.
- Smaller, more transparent runtime; nothing collects behind your back.

**Zig reference cons**

- Every fallible call and every allocation is manual; concurrency must be built
  from state machines.
- No cross-platform test harness as easy as the Go one; the reference's Tracy
  zones were dropped, so observability differs.
- A real game ported to it (ZEROMAN) carries all its state as globals/BSS, which
  the Go lifecycle replaces with fresh construction per launch.

### 3.6 The takeaway

The port kept the reference's *architecture* — one core runs the app, memory is
a fixed budget, failures end in a hang that a reset clears — and swapped the
*expression*: manual, explicit Zig for scheduled goroutines and a managed heap.
The two teaching carts are where that swap becomes visible and, ideally,
memorable. If you take one rule from this page, take the one both architectures
already share: **keep the yield points explicit and the live set bounded.** The
TASKS stall and the HEAP OOM are just that rule, broken on purpose, with a reset
button attached.
