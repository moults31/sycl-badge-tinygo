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
	c.cal.offX = 12
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

// testCard builds a tiny valid 2x2 CardAsset.
func testCard(name string, mask []byte) CardAsset {
	return CardAsset{
		Name:    name,
		Set:     "TEST",
		Types:   []string{"Fire"},
		MaskW:   2,
		MaskH:   2,
		Mask:    mask,
		Palette: []uint16{RGB565(0, 0, 0), RGB565(255, 80, 0), RGB565(255, 255, 255)},
		Ambient: RGB565(24, 10, 4),
	}
}

// TestCardShowUsesInjectedLibrary proves the host sim can drive the cart with
// cards loaded at runtime instead of the baked set.
func TestCardShowUsesInjectedLibrary(t *testing.T) {
	p := newCardPlatform()
	injected := []CardAsset{testCard("INJECTED", []byte{0xF0, 0xFF})}

	c := NewCardShowWith(injected).(*CardShow)
	c.Start(p)
	if len(c.list) != 1 || c.card.Name != "INJECTED" {
		t.Fatalf("injected library not used: list=%d card=%q", len(c.list), c.card.Name)
	}

	c.Update(p)
	lit := 0
	for _, v := range p.frame {
		if v != 0 {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("injected card produced no glow")
	}
}

// TestCardShowWithNilFallsBackToBaked keeps the firmware path: an empty library
// must fall back to the baked cards.
func TestCardShowWithNilFallsBackToBaked(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShowWith(nil).(*CardShow)
	c.Start(p)
	if len(c.list) == 0 {
		t.Fatal("nil library did not fall back to baked cards")
	}
	if len(c.list) != len(cards) {
		t.Fatalf("fallback library = %d cards, want baked %d", len(c.list), len(cards))
	}
}

// TestCardShowCyclesInjectedLibrary checks Left/Right cycle within the injected
// set and wrap around, never reaching the baked cards.
func TestCardShowCyclesInjectedLibrary(t *testing.T) {
	p := newCardPlatform()
	injected := []CardAsset{
		testCard("A", []byte{0xF0, 0xFF}),
		testCard("B", []byte{0x0F, 0x00}),
	}
	c := NewCardShowWith(injected).(*CardShow)
	c.Start(p)

	press := func(b Buttons) {
		p.prev = Buttons{}
		p.buttons = b
		c.Update(p)
		p.prev = p.buttons
		p.buttons = Buttons{}
		c.Update(p)
	}

	press(Buttons{Right: true})
	if c.card.Name != "B" {
		t.Fatalf("Right: card=%q, want B", c.card.Name)
	}
	press(Buttons{Right: true})
	if c.card.Name != "A" {
		t.Fatalf("Right wrap: card=%q, want A", c.card.Name)
	}
	press(Buttons{Left: true})
	if c.card.Name != "B" {
		t.Fatalf("Left wrap: card=%q, want B", c.card.Name)
	}
}
