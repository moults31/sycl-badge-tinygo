package cartridge

import (
	"strings"
	"testing"
)

type fakeEnv struct {
	cur  Buttons
	t    uint32
	step uint32
}

func (e *fakeEnv) Buttons() Buttons { return e.cur }
func (e *fakeEnv) Millis() uint32   { return e.t }
func (e *fakeEnv) Sleep(ms uint32)  { e.t += ms }

type fakeDisplay struct {
	last      []uint16
	frames    int
	backlight uint8
}

func (d *fakeDisplay) Present(f []uint16) {
	d.last = append(d.last[:0], f...)
	d.frames++
}

func (d *fakeDisplay) SetBacklight(level uint8) { d.backlight = level }

func newTestRunner(lib []Factory) (*Runner, *fakeEnv, *fakeDisplay) {
	env := &fakeEnv{step: 16}
	disp := &fakeDisplay{}
	r := NewRunner(env, disp, lib)
	r.SetFrameMillis(16)
	return r, env, disp
}

func step(env *fakeEnv, r *Runner, b Buttons, n int) {
	env.cur = b
	for i := 0; i < n; i++ {
		r.Step()
	}
}

func plasmaLib() []Factory {
	return []Factory{
		{Name: "PLASMA", New: NewPlasma},
		{Name: "PANIC TEST", New: NewPanicTest},
	}
}

func TestLaunchExitAndRelaunch(t *testing.T) {
	r, env, _ := newTestRunner(plasmaLib())

	step(env, r, Buttons{}, 3)
	if r.state != stMenu {
		t.Fatalf("expected menu, got %v", r.state)
	}

	step(env, r, Buttons{A: true}, 1)
	step(env, r, Buttons{}, 1)
	if r.state != stRun {
		t.Fatalf("expected run after A, got %v", r.state)
	}

	// Hold the exit chord. It must take 250 ms, i.e. more than 15 frames.
	step(env, r, Buttons{Start: true, Select: true}, 16)
	if r.state != stRun {
		t.Fatalf("exited after only 16 chord frames")
	}
	step(env, r, Buttons{Start: true, Select: true}, 1)
	if r.state != stMenu {
		t.Fatalf("expected menu after chord, got %v", r.state)
	}
}

func TestFreshStartIsDeterministic(t *testing.T) {
	p := &Platform{frame: make([]uint16, Width*Height)}

	a := NewPlasma()
	a.Start(p)
	a.Update(p)
	first := append([]uint16(nil), p.frame...)

	b := NewPlasma()
	b.Start(p)
	b.Update(p)
	for i := range first {
		if first[i] != p.frame[i] {
			t.Fatalf("relaunch differs at pixel %d: %04x vs %04x", i, first[i], p.frame[i])
		}
	}
}

func TestPanicRecoversToMenu(t *testing.T) {
	r, env, _ := newTestRunner(plasmaLib())

	step(env, r, Buttons{}, 1)
	step(env, r, Buttons{Down: true}, 1)
	if r.sel != 1 {
		t.Fatalf("expected selection 1, got %d", r.sel)
	}

	step(env, r, Buttons{A: true}, 1)
	step(env, r, Buttons{}, 1)
	if r.state != stMenu {
		t.Fatalf("expected menu after panic, got %v", r.state)
	}
	if !strings.Contains(r.crash, "forced panic") {
		t.Fatalf("expected crash text, got %q", r.crash)
	}
}

func TestChordHeldAcrossLaunchDoesNotExit(t *testing.T) {
	r, env, _ := newTestRunner(plasmaLib())

	step(env, r, Buttons{}, 1)

	// Launch while Start+Select are already held.
	step(env, r, Buttons{A: true, Start: true, Select: true}, 2)

	// Keep holding: the chord must stay disarmed until released.
	step(env, r, Buttons{Start: true, Select: true}, 40)
	if r.state != stRun {
		t.Fatalf("held chord bounced to menu; state=%v", r.state)
	}

	// Release (arms), then hold again for 250 ms.
	step(env, r, Buttons{}, 1)
	step(env, r, Buttons{Start: true, Select: true}, 17)
	if r.state != stMenu {
		t.Fatalf("expected exit after a fresh chord, got %v", r.state)
	}
}

func TestMenuSelectsHighlightedCart(t *testing.T) {
	r, env, _ := newTestRunner(plasmaLib())

	step(env, r, Buttons{Down: true}, 1)
	step(env, r, Buttons{}, 1)
	step(env, r, Buttons{A: true}, 1)
	step(env, r, Buttons{}, 1)
	// The panic cart panicked, so we are back at the menu with crash text;
	// had selection stayed on plasma we would still be running.
	if r.state != stMenu || !strings.Contains(r.crash, "forced panic") {
		t.Fatalf("expected panic cart launch, state=%v crash=%q", r.state, r.crash)
	}
}

// TestBootCartLaunchesNamedCart checks BootCart brings the badge up inside the
// named cart, and that the usual exit chord still reaches the menu.
func TestBootCartLaunchesNamedCart(t *testing.T) {
	lib := []Factory{
		{Name: "PLASMA", New: NewPlasma},
		{Name: "CARD SHOW", New: NewCardShow},
	}
	r, env, _ := newTestRunner(lib)
	r.BootCart("CARD SHOW")

	// First step runs Start and switches into the cart.
	step(env, r, Buttons{}, 2)
	if r.state != stRun {
		t.Fatalf("boot state = %v, want running", r.state)
	}
	if got := r.active.Name(); got != "CARD SHOW" {
		t.Fatalf("boot cart = %q, want CARD SHOW", got)
	}

	// Start+Select still exits to the cart menu.
	step(env, r, Buttons{Start: true, Select: true}, 20)
	step(env, r, Buttons{}, 2)
	if r.state != stMenu {
		t.Fatalf("exit chord from boot cart left state %v, want menu", r.state)
	}
}

// TestMenuScrollsToSelection checks the scrolling window follows the highlight
// once the library is taller than the visible rows.
func TestMenuScrollsToSelection(t *testing.T) {
	var lib []Factory
	for i := 0; i < menuVisRows+3; i++ {
		lib = append(lib, Factory{Name: "CART", New: NewPlasma})
	}
	r, env, disp := newTestRunner(lib)

	// Walk to the last row.
	for i := 1; i < len(lib); i++ {
		step(env, r, Buttons{Down: true}, 1)
		step(env, r, Buttons{}, 1)
	}
	if r.sel != len(lib)-1 {
		t.Fatalf("selection = %d, want %d", r.sel, len(lib)-1)
	}
	if r.menuTop != len(lib)-menuVisRows {
		t.Fatalf("menuTop = %d, want %d", r.menuTop, len(lib)-menuVisRows)
	}

	// The selected row must be inside the drawn window.
	if r.sel < r.menuTop || r.sel >= r.menuTop+menuVisRows {
		t.Fatalf("selected row %d outside window [%d,%d)", r.sel, r.menuTop, r.menuTop+menuVisRows)
	}

	// Frame should be non-blank.
	if allZero(disp.last) {
		t.Fatal("scrolled menu frame is blank")
	}
}

// TestBootCartUnknownFallsBackToMenu checks an unknown boot name is ignored.
func TestBootCartUnknownFallsBackToMenu(t *testing.T) {
	r, env, _ := newTestRunner(plasmaLib())
	r.BootCart("NOPE")
	step(env, r, Buttons{}, 2)
	if r.state != stMenu {
		t.Fatalf("unknown BootCart left state %v, want menu", r.state)
	}
}
