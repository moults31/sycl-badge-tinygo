package cartridge

import (
	"runtime"
	"sync"
	"time"
)

// TASKS is a show-and-tell goroutine demonstrator. It is not a game and not
// production code: it exists to make TinyGo's cooperative task scheduler
// visible on the panel.
//
// Four worker goroutines each do a small unit of work and report it; the cart's
// own Update (the frame loop) is itself a goroutine, so "MAIN" is the fifth
// participant. The strip along the bottom is the order reports actually
// arrived, which shows the scheduler interleaving the workers.
//
// The teaching points it makes concrete:
//
//   - runtime.NumCPU() is 1 here even though the RP2350 has two cores. The
//     tasks scheduler is cooperative and has no parallelism; core 1 is reserved
//     for the GC's stop-the-world phase, not application work.
//   - Goroutines only switch at a blocking point (channel op, time.Sleep,
//     mutex, runtime.Gosched). A worker that busy-loops with no yield blocks
//     every other goroutine, including the frame loop, until it finishes.
//   - A panic in a spawned goroutine is NOT caught by the runner's recover
//     (recover only unwinds the panicking goroutine's own stack). On hardware
//     it aborts the chip; recovery is a reset.
//
// Workers exit on their own when the cart stops being updated: they watch a
// heartbeat the frame loop bumps, and when it stops changing they return. That
// keeps the Cartridge contract at Start+Update without leaking goroutines (and
// their stacks) across relaunches.
type Tasks struct {
	// Worker coordination. hb/alive/greedy are guarded because the workers
	// run as separate goroutines even on the host, where the race detector is
	// watching.
	mu     sync.Mutex
	hb     uint32
	alive  int
	greedy bool
	report chan int

	// Worker tuning. Defaults are set in Start; the unit tests shorten them.
	workerSleep time.Duration
	staleTicks  int

	// Main-goroutine state, touched only by Update/render.
	counts [tasksWorkers]uint64
	trace  [tasksTraceN]int8
	traceI int
	traceN int
	stalls uint64
	t      uint32

	greedyFlash uint8
	stallFlash  uint8
	panicArm    uint8
	simFatal    uint8

	gcN     uint32
	lastGC  uint32
	gcFlash uint8
}

// NewTasks returns the goroutine demonstrator.
func NewTasks() Cartridge { return &Tasks{} }

// Name implements Cartridge.
func (c *Tasks) Name() string { return "TASKS" }

const (
	tasksWorkers   = 4
	tasksTraceN    = 40
	tasksSweep     = 6000 // bar wraps after this many reported units
	tasksReportCap = 128
	tasksStaleDef  = 40 // ~0.7s of unchanged heartbeat at the default pace
)

// Start rebuilds all per-launch state and spawns the workers. The runtime
// constructs a fresh Tasks before every Start, so this is the whole state.
func (c *Tasks) Start(p *Platform) {
	if c.workerSleep == 0 {
		c.workerSleep = 18 * time.Millisecond
	}
	if c.staleTicks == 0 {
		c.staleTicks = tasksStaleDef
	}
	c.report = make(chan int, tasksReportCap)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	c.lastGC = m.NumGC
	c.gcN = m.NumGC

	c.mu.Lock()
	c.hb++
	c.alive = 0
	c.greedy = false
	c.mu.Unlock()

	for i := 0; i < tasksWorkers; i++ {
		go c.worker(i)
	}
}

// Update implements Cartridge.
func (c *Tasks) Update(p *Platform) {
	c.t++
	c.mu.Lock()
	c.hb++
	c.mu.Unlock()

	// A deliberately simulated fatal on the host: the real thing would abort
	// the process, so the sim shows the same screen and moves on.
	if c.simFatal > 0 {
		c.simFatal--
		p.clear(RGB565(0x50, 0x00, 0x00))
		p.drawText(8, 40, "GOROUTINE PANIC", colText, RGB565(0x50, 0, 0))
		p.drawText(8, 56, "would abort() here", colWarn, RGB565(0x50, 0, 0))
		p.drawText(8, 72, "(host sim: capped)", colDim, RGB565(0x50, 0, 0))
		return
	}

	// Drain reports. -1 marks a completed greedy (no-yield) burst; it is how
	// the frame loop learns a stall happened even though it was frozen for it.
drain:
	for {
		select {
		case id := <-c.report:
			if id < 0 {
				c.stalls++
				c.stallFlash = 40
			} else if id < tasksWorkers {
				c.counts[id]++
				c.pushTrace(id)
			}
		default:
			break drain
		}
	}

	// The exit chord is Start+Select, so A and B are free for the demo.
	if pressed(p.buttons.A, p.prev.A) {
		c.greedy = !c.greedy
		c.greedyFlash = 60
	}
	if pressed(p.buttons.B, p.prev.B) {
		if c.panicArm == 0 {
			c.panicArm = 180 // ~3s to press B again
		} else {
			c.panicArm = 0
			if onBaremetal {
				// Panics on a fresh goroutine's stack: no recover in scope.
				go func() { panic("panic in a goroutine: recover is per-goroutine") }()
			} else {
				c.simFatal = 90
			}
		}
	}
	if c.panicArm > 0 {
		c.panicArm--
	}
	flashDec(&c.greedyFlash)
	flashDec(&c.stallFlash)

	// Sample the GC counter occasionally to light the "core 1" indicator.
	if c.t%20 == 0 {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if m.NumGC != c.lastGC {
			c.gcFlash = 30
		}
		c.lastGC = m.NumGC
		c.gcN = m.NumGC
	}
	flashDec(&c.gcFlash)

	c.render(p)
}

func (c *Tasks) pushTrace(id int) {
	c.trace[c.traceI] = int8(id)
	c.traceI = (c.traceI + 1) % tasksTraceN
	if c.traceN < tasksTraceN {
		c.traceN++
	}
}

// worker is one goroutine. It does a unit of work, reports it, and yields by
// sleeping. The stale-heartbeat check is what ends the worker after the cart is
// no longer being updated (exit, or a recovered panic).
func (c *Tasks) worker(id int) {
	c.mu.Lock()
	c.alive++
	c.mu.Unlock()

	// Snapshot the heartbeat once the worker is registered. It is distinct
	// from the Start-time value, so the worker does not start already stale.
	last := c.heartbeat()

	idle := 0
	acc := uint32(2463534242)

	for {
		for i := 0; i < 400; i++ {
			acc = acc*1664525 + 1013904223 + uint32(i)
		}
		if c.greedyFor(id) {
			// Busy-loop with no blocking call: on a cooperative scheduler this
			// starves every other goroutine, including the frame loop, until
			// the wall-clock budget runs out.
			start := time.Now()
			for time.Since(start) < 300*time.Millisecond {
				acc = acc*1664525 + 1013904223
			}
			c.noteReport(-1)
		}
		c.noteReport(id)

		time.Sleep(c.workerSleep)

		if cur := c.heartbeat(); cur == last {
			idle++
		} else {
			idle, last = 0, cur
		}
		if idle > c.staleTicks {
			break
		}
	}
	_ = acc

	c.mu.Lock()
	c.alive--
	c.mu.Unlock()
}

// noteReport sends a report without blocking. A full channel just drops the
// sample; the demo only needs a representative stream.
func (c *Tasks) noteReport(id int) {
	select {
	case c.report <- id:
	default:
	}
}

func (c *Tasks) heartbeat() uint32 {
	c.mu.Lock()
	v := c.hb
	c.mu.Unlock()
	return v
}

func (c *Tasks) aliveCount() int {
	c.mu.Lock()
	v := c.alive
	c.mu.Unlock()
	return v
}

func (c *Tasks) greedyFor(id int) bool {
	c.mu.Lock()
	v := c.greedy && id == 1
	c.mu.Unlock()
	return v
}

// --- rendering ---

var (
	colWarn  = RGB565(0xFF, 0xB0, 0x30)
	colTrace = [tasksWorkers]uint16{
		RGB565(0x50, 0xC8, 0xF0), // G0 cyan
		RGB565(0x60, 0xE0, 0x60), // G1 green
		RGB565(0xF0, 0x80, 0xC0), // G2 pink
		RGB565(0xFF, 0xE0, 0x50), // G3 yellow
	}
	colStall = RGB565(0xFF, 0xFF, 0xFF)
	colLane  = RGB565(0x30, 0x3C, 0x50)
)

func (c *Tasks) render(p *Platform) {
	p.clear(colBg)

	p.drawText(4, 2, "TASKS", colAccent, colBg)
	p.drawText(116, 2, "CPU 1", colDim, colBg)
	p.drawText(4, 14, "COOPERATIVE", colDim, colBg)

	gcCol := colDim
	if c.gcFlash > 0 {
		gcCol = colAccent
	}
	p.drawText(120, 14, "GC", gcCol, colBg)
	drawUint(p, 140, 14, uint64(c.gcN), 3, gcCol, colBg)

	for i := 0; i < tasksWorkers; i++ {
		y := 26 + i*14
		p.drawGlyph(4, y, 'G', colText, colBg)
		p.drawGlyph(12, y, byte('0'+i), colText, colBg)
		p.fillRect(26, y, 94, 8, colLane)
		w := int(c.counts[i]%tasksSweep) * 94 / tasksSweep
		if w > 0 {
			p.fillRect(26, y, w, 8, colTrace[i])
		}
		drawUint(p, 124, y, c.counts[i]%10000, 4, colText, colBg)
	}

	p.drawText(4, 80, "SCHED", colDim, colBg)
	start := (c.traceI - c.traceN + tasksTraceN) % tasksTraceN
	for k := 0; k < c.traceN; k++ {
		id := c.trace[(start+k)%tasksTraceN]
		col := colLane
		switch {
		case id == -1:
			col = colStall
		case id >= 0 && id < tasksWorkers:
			col = colTrace[id]
		}
		p.fillRect(k*4, 90, 4, 8, col)
	}

	status := ""
	col := colDim
	switch {
	case c.panicArm > 0:
		status, col = "B AGAIN = GO PANIC", colErr
	case c.greedy && c.stallFlash > 0:
		status, col = "STALL: NO PREEMPT", colStall
	case c.greedy:
		status, col = "GREEDY G1 NO YIELD", colWarn
	case c.stallFlash > 0:
		status, col = "STALLED, THEN CATCHUP", colWarn
	case c.gcFlash > 0:
		status, col = "GC: CORE 1 PAUSED", colAccent
	}
	if status != "" {
		p.drawText(4, 104, status, col, colBg)
	}

	p.drawText(8, 116, "A GREEDY  B PANIC", colDim, colBg)
}
