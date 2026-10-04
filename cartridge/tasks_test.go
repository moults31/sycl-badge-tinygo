package cartridge

import (
	"testing"
	"time"
)

// It can take a worker a moment to notice its heartbeat stopped. Poll instead
// of assuming an exact frame count.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	if t != nil {
		t.Helper()
	}
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

func newTasks() *Tasks {
	c := &Tasks{workerSleep: time.Millisecond, staleTicks: 4}
	return c
}

func TestTasksSpawnsAndDrawsWorkers(t *testing.T) {
	c := newTasks()
	p := &Platform{frame: make([]uint16, Width*Height)}
	c.Start(p)
	defer c.stop()

	if !waitFor(t, time.Second, func() bool { return c.aliveCount() == tasksWorkers }) {
		t.Fatalf("alive workers = %d, want %d", c.aliveCount(), tasksWorkers)
	}

	// Let reports trickle in (draining them as frames would), then step a
	// frame and check the rendered frame is not blank.
	if !waitFor(t, 2*time.Second, func() bool {
		c.updateFrames(p, 1)
		return c.progressed()
	}) {
		t.Fatal("no worker reports arrived")
	}
	if allZero(p.frame) {
		t.Fatal("tasks frame is blank")
	}
}

func TestTasksWorkersExitAfterHeartbeatStops(t *testing.T) {
	c := newTasks()
	p := &Platform{frame: make([]uint16, Width*Height)}
	c.Start(p)

	// One update to bump the heartbeat, then stop updating entirely.
	c.updateFrames(p, 1)

	if !waitFor(t, 3*time.Second, func() bool { return c.aliveCount() == 0 }) {
		t.Fatalf("workers did not exit: %d still alive", c.aliveCount())
	}
}

func TestTasksPanicArmNeedsSecondPress(t *testing.T) {
	c := newTasks()
	p := &Platform{frame: make([]uint16, Width*Height), prev: Buttons{}}
	c.Start(p)
	defer c.stop()

	// First B arms but must not fire.
	p.buttons = Buttons{B: true}
	c.Update(p)
	if c.panicArm == 0 {
		t.Fatal("first B did not arm the panic")
	}
	if c.simFatal != 0 {
		t.Fatal("first B should not fire the panic")
	}

	// Release, then press again: on the host this sets the simulated fatal.
	p.prev = p.buttons
	p.buttons = Buttons{}
	c.Update(p)
	p.prev = p.buttons
	p.buttons = Buttons{B: true}
	c.Update(p)
	if c.simFatal == 0 {
		t.Fatal("second B did not fire the (host) panic")
	}
}

func (c *Tasks) updateFrames(p *Platform, n int) {
	for i := 0; i < n; i++ {
		c.Update(p)
	}
}

func (c *Tasks) progressed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.counts {
		if n > 0 {
			return true
		}
	}
	return c.stalls > 0
}

// stop makes the workers return promptly so a test cannot leak goroutines.
// Bumping the heartbeat would reset the workers' stale counters, so instead
// shrink the window and let the unchanged counter trip it.
func (c *Tasks) stop() {
	c.mu.Lock()
	c.staleTicks = 0
	c.mu.Unlock()
	waitFor(nil, 3*time.Second, func() bool { return c.aliveCount() == 0 })
}

func allZero(f []uint16) bool {
	for _, v := range f {
		if v != 0 {
			return false
		}
	}
	return true
}
