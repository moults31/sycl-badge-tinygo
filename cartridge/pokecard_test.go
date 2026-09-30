package cartridge

import "testing"

func newCardPlatform() *Platform {
	return &Platform{frame: make([]uint16, Width*Height), backlight: 255}
}

func cloneFrame(p *Platform) []uint16 {
	return append([]uint16(nil), p.frame...)
}

func framesEqual(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCardShowDeterministic(t *testing.T) {
	p := newCardPlatform()

	a := NewCardShow()
	a.Start(p)
	a.Update(p)
	first := cloneFrame(p)

	b := NewCardShow()
	b.Start(p)
	b.Update(p)
	if !framesEqual(first, p.frame) {
		t.Fatal("fresh CardShow Start/Update is not deterministic")
	}
}

func TestCardShowHasGlow(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShow()
	c.Start(p)
	c.Update(p)

	lit := 0
	for _, v := range p.frame {
		if v != 0 {
			lit++
		}
	}
	if lit < 1000 {
		t.Fatalf("expected a visible glow, only %d lit pixels", lit)
	}
}

func TestCardShowCalibrationChangesFrame(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShow().(*CardShow)
	c.Start(p)

	c.render(p)
	base := cloneFrame(p)

	c.calib = true
	c.render(p)
	if framesEqual(base, p.frame) {
		t.Fatal("calibration overlay did not change the frame")
	}

	c.calib = false
	c.offX = 12
	c.render(p)
	if framesEqual(base, p.frame) {
		t.Fatal("offset trim did not change the frame")
	}
}

func TestCardShowDrivesBacklight(t *testing.T) {
	lib := []Factory{{Name: "CARD SHOW", New: NewCardShow}}
	r, env, disp := newTestRunner(lib)

	// Launch.
	step(env, r, Buttons{A: true}, 1)
	step(env, r, Buttons{}, 2)

	sawDim := false
	for i := 0; i < 240; i++ {
		step(env, r, Buttons{}, 1)
		if disp.backlight > 0 && disp.backlight < 255 {
			sawDim = true
		}
	}
	if !sawDim {
		t.Fatal("backlight never breathed below full during idle")
	}

	// Let any flash decay, then trigger a fresh attack: it must pin to full.
	step(env, r, Buttons{}, 60)
	step(env, r, Buttons{A: true}, 1)
	step(env, r, Buttons{}, 1)
	if disp.backlight != 255 {
		t.Fatalf("attack backlight = %d, want 255", disp.backlight)
	}
}
