package cartridge

import "testing"

func newHeap() *Heap { return &Heap{} }

func TestHeapStartSamplesStats(t *testing.T) {
	c := newHeap()
	p := &Platform{frame: make([]uint16, Width*Height)}
	c.Start(p)

	// Start forces one sample; the live heap must be non-zero on any real
	// runtime.
	if c.stats.HeapInuse == 0 {
		t.Fatal("HeapInuse is zero after Start; ReadMemStats did not populate")
	}
	if c.stats.HeapSys == 0 {
		t.Fatal("HeapSys is zero after Start")
	}
}

func TestHeapAllocAndFreeMovesLiveBytes(t *testing.T) {
	c := newHeap()
	p := &Platform{frame: make([]uint16, Width*Height), prev: Buttons{}}
	c.Start(p)

	before := c.stats.HeapInuse

	// A allocates and retains.
	p.buttons = Buttons{A: true}
	c.Update(p)
	if c.retained == 0 {
		t.Fatal("A did not retain bytes")
	}
	p.prev = p.buttons
	p.buttons = Buttons{}
	c.Update(p)
	if c.stats.HeapInuse <= before {
		t.Fatalf("live heap did not grow: %d -> %d", before, c.stats.HeapInuse)
	}

	// B frees and collects.
	grown := c.stats.HeapInuse
	p.prev = p.buttons
	p.buttons = Buttons{B: true}
	c.Update(p)
	if c.retained != 0 {
		t.Fatal("B did not drop the retained chunks")
	}
	if c.stats.HeapInuse >= grown {
		t.Fatalf("live heap did not shrink after free+GC: %d -> %d", grown, c.stats.HeapInuse)
	}
}

func TestHeapOOMCapsOnHost(t *testing.T) {
	c := newHeap()
	c.maxAlloc = 128 << 10 // small cap so the test is fast
	p := &Platform{frame: make([]uint16, Width*Height), prev: Buttons{}}
	c.Start(p)

	// Hold A+B past the arm window to trigger the exhaustion demo.
	p.buttons = Buttons{A: true, B: true}
	for i := 0; i < heapArmFrames+5 && !c.oom; i++ {
		c.Update(p)
	}
	if !c.oom {
		t.Fatal("holding A+B did not arm the OOM demo")
	}

	// Keep going; on the host the demo must stop at the cap rather than
	// allocating without bound.
	for i := 0; i < 200 && c.oom; i++ {
		c.Update(p)
	}
	if c.oom {
		t.Fatal("host OOM demo did not stop at maxAlloc")
	}
	if c.retained > c.maxAlloc+heapOOMStep {
		t.Fatalf("retained %d exceeded cap %d", c.retained, c.maxAlloc)
	}
}

func TestHeapLeakToggleAllocatesPerFrame(t *testing.T) {
	c := newHeap()
	p := &Platform{frame: make([]uint16, Width*Height), prev: Buttons{}}
	c.Start(p)

	// Click toggles the leak.
	p.buttons = Buttons{Click: true}
	c.Update(p)
	if !c.leak {
		t.Fatal("stick click did not enable the leak")
	}
	p.prev = p.buttons
	p.buttons = Buttons{}
	first := c.retained
	c.Update(p)
	c.Update(p)
	if c.retained <= first {
		t.Fatalf("leak did not accumulate per frame: %d -> %d", first, c.retained)
	}
}
