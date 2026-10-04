package cartridge

import "runtime"

// HEAP is a show-and-tell garbage-collector visualiser. Like TASKS it is a
// teaching aid, not production code.
//
// It draws the TinyGo heap as a bar (live vs. total) using runtime.ReadMemStats,
// which works against the baremetal conservative block collector. Buttons let
// you allocate and retain, drop references and collect, or leak continuously,
// and the bar plus the GC/OBJ counters show what the collector can and cannot
// reclaim.
//
// The headline demonstration is the button chord: holding A+B keeps allocating
// retained chunks until the allocator cannot find a free range, the collector
// cannot free enough, and growHeap() fails. On baremetal growHeap() is a hard
// false, so that path ends in runtimeFatal("out of memory") -> abort() -> a
// locked-up wfi loop. It is deliberately NOT a Go panic and NOT recoverable by
// the runner's recover: the only way back is a reset, after which the badge
// boots normally. On the host build the demo caps itself so the sim process is
// not killed.
type Heap struct {
	keep     [][]byte
	retained uint64

	// maxAlloc is the safety ceiling for the exhaustion demo. On hardware the
	// real heap is far smaller, so OOM happens first; on the host it stops the
	// demo before the OS process is harmed.
	maxAlloc uint64

	leak bool
	oom  bool
	arm  int

	stats   runtime.MemStats
	lastGC  uint32
	gcFlash uint8
	force   bool

	msg  string
	msgT uint8
	t    uint32
}

// NewHeap returns the GC/heap visualiser.
func NewHeap() Cartridge { return &Heap{} }

// Name implements Cartridge.
func (c *Heap) Name() string { return "HEAP" }

const (
	heapChunk     = 16 << 10 // bytes retained per A press
	heapLeakStep  = 8 << 10  // bytes retained per frame while leaking
	heapOOMStep   = 32 << 10 // bytes retained per frame in the OOM demo
	heapOOMCap    = 4 << 20  // host safety ceiling; hardware is far smaller
	heapArmFrames = 45       // frames of A+B before the OOM demo starts
	heapSample    = 5        // frames between ReadMemStats samples
)

// Start rebuilds the per-launch state. A fresh Heap is constructed per launch,
// so this is the whole state.
func (c *Heap) Start(p *Platform) {
	c.keep = nil
	c.retained = 0
	if c.maxAlloc == 0 {
		c.maxAlloc = heapOOMCap
	}
	c.leak, c.oom, c.arm = false, false, 0
	c.msg, c.msgT = "", 0
	c.force = true
	c.sample()
	c.gcFlash = 0
}

// Update implements Cartridge.
func (c *Heap) Update(p *Platform) {
	c.t++

	both := p.buttons.A && p.buttons.B
	if both {
		if c.arm < heapArmFrames {
			c.arm++
		}
		if c.arm >= heapArmFrames && !c.oom {
			c.oom = true
			c.msg, c.msgT = "EXHAUSTING HEAP", 255
			if !onBaremetal {
				// The host has a real OS heap to draw from; keep the demo
				// bounded so the sim survives. Hardware never gets here.
				c.arm = 0
			}
		}
	} else {
		c.arm = 0
	}

	if !c.oom {
		if pressed(p.buttons.A, p.prev.A) && !both {
			c.alloc(heapChunk)
			c.msg, c.msgT = "ALLOC +16K", 40
		}
		if pressed(p.buttons.B, p.prev.B) && !both {
			c.freeAll()
			c.msg, c.msgT = "RELEASE + GC", 40
		}
		if pressed(p.buttons.Click, p.prev.Click) {
			c.leak = !c.leak
			if c.leak {
				c.msg = "LEAK ON"
			} else {
				c.msg = "LEAK OFF"
			}
			c.msgT = 40
		}
		if c.leak {
			c.alloc(heapLeakStep)
		}
	} else {
		// Keep allocating and retaining. On baremetal this reaches
		// runtimeFatal before maxAlloc; on the host it stops at the cap.
		c.alloc(heapOOMStep)
		if c.retained > c.maxAlloc {
			c.oom = false
			c.msg, c.msgT = "HOST CAPPED SAFELY", 255
		}
	}

	if c.msgT > 0 {
		c.msgT--
	} else {
		c.msg = ""
	}
	flashDec(&c.gcFlash)

	c.sample()
	c.render(p)
}

func (c *Heap) alloc(n int) {
	b := make([]byte, n)
	b[0] = byte(c.t) // touch it so the allocation is real
	c.keep = append(c.keep, b)
	c.retained += uint64(n)
	c.force = true
}

func (c *Heap) freeAll() {
	c.keep = nil
	c.retained = 0
	runtime.GC()
	c.force = true
}

func (c *Heap) sample() {
	if !c.force && c.t%heapSample != 0 {
		return
	}
	c.force = false
	runtime.ReadMemStats(&c.stats)
	if c.stats.NumGC != c.lastGC {
		c.gcFlash = 20
	}
	c.lastGC = c.stats.NumGC
}

// --- rendering ---

func (c *Heap) render(p *Platform) {
	p.clear(colBg)
	p.drawText(4, 2, "HEAP", colAccent, colBg)
	p.drawText(124, 2, "GO GC", colDim, colBg)

	// Heap bar: live blocks over usable heap.
	const bx, by, bw, bh = 4, 18, Width - 8, 14
	used, total := c.stats.HeapInuse, c.stats.HeapSys
	p.fillRect(bx, by, bw, bh, colLane)
	frac := 0
	if total > 0 {
		frac = int(used * uint64(bw) / total)
	}
	if frac > bw {
		frac = bw
	}
	fill := RGB565(0x50, 0xD0, 0x60)
	switch {
	case total > 0 && used*4 > total*3:
		fill = colErr
	case total > 0 && used*2 > total:
		fill = colWarn
	}
	if frac > 0 {
		p.fillRect(bx, by, frac, bh, fill)
	}
	if c.gcFlash > 0 {
		p.fillRect(bx, by, bw, 1, colAccent)
		p.fillRect(bx, by+bh-1, bw, 1, colAccent)
	}

	p.drawText(4, 36, "LIVE", colText, colBg)
	x := drawKB(p, 44, 36, clampKB(used), colText, colBg)
	p.drawGlyph(x, 36, '/', colDim, colBg)
	drawKB(p, x+8, 36, clampKB(total), colDim, colBg)

	p.drawText(4, 50, "GC", colText, colBg)
	drawUint(p, 28, 50, uint64(c.stats.NumGC), 3, colText, colBg)
	p.drawText(72, 50, "OBJ", colText, colBg)
	drawUint(p, 104, 50, c.stats.HeapObjects, 4, colText, colBg)

	p.drawText(4, 64, "TOT", colText, colBg)
	drawKB(p, 36, 64, clampKB(c.stats.TotalAlloc), colDim, colBg)
	p.drawText(84, 64, "HELD", colText, colBg)
	drawKB(p, 116, 64, clampKB(c.retained), colWarn, colBg)

	status, col := c.msg, colAccent
	switch {
	case c.oom:
		status, col = "!! OOM IN PROGRESS", colErr
	case c.arm > 0:
		status, col = "HOLD... OOM ARMED", colErr
	case status == "":
		status, col = "ALLOC-DRIVEN GC", colDim
	}
	if len(status) > 20 {
		status = status[:20]
	}
	p.drawText(4, 84, status, col, colBg)
	if c.arm > 0 {
		aw := c.arm * (Width - 8) / heapArmFrames
		p.fillRect(4, 96, aw, 4, colErr)
	}

	p.drawText(4, 106, "A ALLOC  B FREE+GC", colDim, colBg)
	p.drawText(4, 118, "CLICK LEAK  A+B OOM", colDim, colBg)
}

// clampKB keeps a value inside four decimal cells so a host build's large
// cumulative counters cannot overrun the panel.
func clampKB(v uint64) uint64 {
	const max = 9999 * 1024
	if v > max {
		return max
	}
	return v
}
