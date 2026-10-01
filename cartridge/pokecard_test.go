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

// TestSwapRB checks the red/blue channel swap at the heart of the panel's BGR
// wiring: red becomes blue, blue becomes red, green is untouched.
func TestSwapRB(t *testing.T) {
	cases := []struct {
		in      uint16
		r, g, b uint8
	}{
		{RGB565(255, 0, 0), 0, 0, 248},
		{RGB565(0, 0, 255), 248, 0, 0},
		{RGB565(0, 255, 0), 0, 252, 0},
	}
	for _, tc := range cases {
		r, g, b := unpack(swapRB(tc.in))
		if r != tc.r || g != tc.g || b != tc.b {
			t.Fatalf("swapRB(%04X) = %d,%d,%d, want %d,%d,%d",
				tc.in, r, g, b, tc.r, tc.g, tc.b)
		}
	}
}

// mapFixture returns a started CardShow with the plasma fields built, for the
// mappings that need them.
func mapFixture() *CardShow {
	c := NewCardShowWith([]CardAsset{testCard("A", []byte{0xF0, 0xFF})}).(*CardShow)
	c.Start(newCardPlatform())
	return c
}

// TestApplyColorMap checks each mapping rewrites the frame as named and that
// the bit rotations invert cleanly.
func TestApplyColorMap(t *testing.T) {
	red := RGB565(255, 0, 0)
	c := mapFixture()

	fb := []uint16{red}
	c.applyColorMap(fb, colorRGB)
	if fb[0] != red {
		t.Fatalf("identity changed the pixel: %04X", fb[0])
	}

	fb = []uint16{red}
	c.applyColorMap(fb, colorBGR)
	if r, g, b := unpack(fb[0]); r != 0 || g != 0 || b != 248 {
		t.Fatalf("BGR map: red -> %d,%d,%d, want blue", r, g, b)
	}

	fb = []uint16{red}
	c.applyColorMap(fb, colorSwap16)
	if got := rot16(red, 8); fb[0] != got {
		t.Fatalf("swap16 map: %04X -> %04X, want %04X", red, fb[0], got)
	}

	fb = []uint16{red}
	c.applyColorMap(fb, colorOil)
	oil := fb[0]
	if oil == red {
		t.Fatalf("OIL map left %04X unchanged", red)
	}
	if oil != rot16(red, cardOilRotate) || rot16(oil, 16-cardOilRotate) != red {
		t.Fatalf("OIL is not rot16(., %d): %04X -> %04X", cardOilRotate, red, oil)
	}
}

// TestHueRotatePreservesLuma checks the hue mapping leaves luminance (which
// carries the artwork's edges) essentially untouched at every phase. That is
// the property that keeps the outlines crisp while the colours move.
func TestHueRotatePreservesLuma(t *testing.T) {
	cols := [][3]uint8{
		{128, 128, 128}, {100, 120, 140}, {160, 110, 90}, {90, 140, 150}, {200, 180, 170},
	}
	for _, c := range cols {
		in := RGB565(c[0], c[1], c[2])
		r0, g0, b0 := unpack(in)
		before := lum8(r0, g0, b0)
		for ph := uint32(0); ph < uint32(cardHuePeriodFrames); ph += 53 {
			fb := []uint16{in}
			applyHueRotate(fb, ph)
			r, g, b := unpack(fb[0])
			if d := absDiff(lum8(r, g, b), before); d > 5 {
				t.Fatalf("hue changed luma of %v at phase %d by %d (%d -> %d)",
					c, ph, d, before, lum8(r, g, b))
			}
		}
	}
}

// TestFluxPreservesLuma checks the plasma FLUX mapping, like HUE, does not
// change luminance: it only rotates hue, so the card's lines survive the
// moving colour.
func TestFluxPreservesLuma(t *testing.T) {
	c := mapFixture()
	cols := [][3]uint8{{128, 128, 128}, {160, 110, 90}, {90, 140, 150}}
	for _, col := range cols {
		in := RGB565(col[0], col[1], col[2])
		r0, g0, b0 := unpack(in)
		before := lum8(r0, g0, b0)
		fb := []uint16{in}
		c.applyFlux(fb)
		r, g, b := unpack(fb[0])
		if d := absDiff(lum8(r, g, b), before); d > 6 {
			t.Fatalf("FLUX changed luma of %v by %d (%d -> %d)", col, d, before, lum8(r, g, b))
		}
	}
}

// lum8 is integer Rec.601 luma of 8-bit components.
func lum8(r, g, b uint8) int {
	return (299*int(r) + 587*int(g) + 114*int(b)) / 1000
}

// TestCardShowTogglesBreathing checks B disables the breathing envelope: with
// it off, the backlight holds steady instead of pulsing.
func TestCardShowTogglesBreathing(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShowWith([]CardAsset{testCard("A", []byte{0xF0, 0xFF})}).(*CardShow)
	c.Start(p)
	if !c.breathe {
		t.Fatal("breathing should default to on")
	}

	// See it breathe: the backlight must vary while left running.
	seen := map[uint8]bool{}
	for i := 0; i < 240; i++ {
		p.prev = p.buttons
		p.buttons = Buttons{}
		c.Update(p)
		seen[p.backlight] = true
	}
	if len(seen) < 2 {
		t.Fatalf("breathing on produced a constant backlight (%d levels)", len(seen))
	}

	// B turns it off.
	p.prev = Buttons{}
	p.buttons = Buttons{B: true}
	c.Update(p)
	if c.breathe {
		t.Fatal("B did not toggle breathing off")
	}
	p.prev = p.buttons
	p.buttons = Buttons{}
	c.Update(p)

	// Now the backlight is steady frame to frame (no attack flash pending).
	var first uint8
	steady := true
	for i := 0; i < 240; i++ {
		p.prev = p.buttons
		p.buttons = Buttons{}
		c.Update(p)
		if i == 0 {
			first = p.backlight
		} else if p.backlight != first {
			steady = false
		}
	}
	if !steady {
		t.Fatal("breathing off still varied the backlight")
	}

	// B again brings it back.
	p.prev = Buttons{}
	p.buttons = Buttons{B: true}
	c.Update(p)
	if !c.breathe {
		t.Fatal("B did not toggle breathing back on")
	}
}

// pressFrame advances one frame with prev cleared, so a single frame of b
// registers as an edge.
func pressFrame(c *CardShow, p *Platform, b Buttons) {
	p.prev = p.buttons
	p.buttons = b
	c.Update(p)
}

// TestCardShowMenuOpensOnStart checks Start opens the menu, A is the flash and
// does not open it, and Start+Select does not open the menu (that is the
// runtime's exit chord).
func TestCardShowMenuOpensOnStart(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShowWith([]CardAsset{testCard("A", []byte{0xF0, 0xFF})}).(*CardShow)
	c.Start(p)

	// A tap flashes and does not open the menu.
	pressFrame(c, p, Buttons{A: true})
	if c.menu {
		t.Fatal("A opened the menu")
	}
	if c.flash == 0 {
		t.Fatal("A did not fire the flash")
	}
	pressFrame(c, p, Buttons{})

	// Start opens the menu.
	pressFrame(c, p, Buttons{Start: true})
	if !c.menu {
		t.Fatal("Start did not open the menu")
	}

	// B closes it.
	pressFrame(c, p, Buttons{B: true})
	pressFrame(c, p, Buttons{})
	if c.menu {
		t.Fatal("B did not close the menu")
	}

	// Start+Select (the exit chord) must not open the menu.
	pressFrame(c, p, Buttons{Start: true, Select: true})
	if c.menu {
		t.Fatal("Start+Select opened the menu")
	}
}

// TestCardShowMenuEditsEffects drives the menu with buttons and checks the
// focused row changes value, and that B closes it.
func TestCardShowMenuEditsEffects(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShowWith([]CardAsset{
		testCard("A", []byte{0xF0, 0xFF}),
		testCard("B", []byte{0x0F, 0x00}),
	}).(*CardShow)
	c.Start(p)

	// Open the menu directly (the Start gesture is covered above).
	c.menu = true
	c.menuRow = int(rowColour)
	start := c.colorMap

	// Right on COLOUR advances the mapping, Left backs it up.
	pressFrame(c, p, Buttons{Right: true})
	pressFrame(c, p, Buttons{})
	if c.colorMap != (start+1)%colorMapCount {
		t.Fatalf("menu Right: colour = %d, want %d", c.colorMap, (start+1)%colorMapCount)
	}
	pressFrame(c, p, Buttons{Left: true})
	pressFrame(c, p, Buttons{})
	if c.colorMap != start {
		t.Fatalf("menu Left: colour = %d, want %d", c.colorMap, start)
	}

	// Down to CARD and Right cycles the library.
	pressFrame(c, p, Buttons{Down: true})
	pressFrame(c, p, Buttons{})
	if c.menuRow != int(rowCard) {
		t.Fatalf("Down moved to row %d, want CARD", c.menuRow)
	}
	name := c.card.Name
	pressFrame(c, p, Buttons{Right: true})
	pressFrame(c, p, Buttons{})
	if c.card.Name == name {
		t.Fatal("menu Right on CARD did not change the card")
	}

	// Down through the boolean rows; A toggles each.
	for _, row := range []struct {
		r cardMenuRow
		v *bool
	}{{rowBreathe, &c.breathe}, {rowHolo, &c.holo}, {rowSparkle, &c.sparkle}} {
		pressFrame(c, p, Buttons{Down: true})
		pressFrame(c, p, Buttons{})
		if c.menuRow != int(row.r) {
			t.Fatalf("menu row = %d, want %d", c.menuRow, row.r)
		}
		before := *row.v
		pressFrame(c, p, Buttons{A: true})
		pressFrame(c, p, Buttons{})
		if *row.v == before {
			t.Fatalf("A did not toggle row %d", row.r)
		}
	}

	// B closes.
	pressFrame(c, p, Buttons{B: true})
	pressFrame(c, p, Buttons{})
	if c.menu {
		t.Fatal("B did not close the menu")
	}
}

// TestCardShowEffectsAreIndependent checks the three effect layers gate their
// own contribution to the frame.
func TestCardShowEffectsAreIndependent(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShowWith([]CardAsset{testCard("A", []byte{0xF0, 0xFF})}).(*CardShow)

	// renderAt sets the toggles, renders one frame and returns a copy.
	renderAt := func(breathe, holo, sparkle bool) []uint16 {
		c.Start(p)
		c.breathe, c.holo, c.sparkle = breathe, holo, sparkle
		c.render(p)
		return cloneFrame(p)
	}

	// All on differs from each variant with one layer disabled.
	all := renderAt(true, true, true)
	if framesEqual(all, renderAt(false, true, true)) {
		t.Fatal("disabling BREATHE changed nothing")
	}
	if framesEqual(all, renderAt(true, false, true)) {
		t.Fatal("disabling HOLO changed nothing")
	}
	if framesEqual(all, renderAt(true, true, false)) {
		t.Fatal("disabling SPARKLE changed nothing")
	}

	// Everything off is a legal, distinct frame (steady art).
	none := renderAt(false, false, false)
	if framesEqual(all, none) {
		t.Fatal("all effects off equals all on")
	}
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// TestCardShowCyclesColorMap checks the joystick click walks the mapping cycle
// and wraps back to identity, and that a non-identity mapping changes the
// finished frame.
func TestCardShowCyclesColorMap(t *testing.T) {
	p := newCardPlatform()
	c := NewCardShowWith([]CardAsset{testCard("A", []byte{0xF0, 0xFF})}).(*CardShow)
	c.Start(p)
	if c.colorMap != colorRGB {
		t.Fatalf("Start mapping = %d, want identity", c.colorMap)
	}

	c.render(p)
	base := cloneFrame(p)

	click := func() {
		p.prev = Buttons{}
		p.buttons = Buttons{Click: true}
		c.Update(p)
		p.prev = p.buttons
		p.buttons = Buttons{}
		c.Update(p)
	}

	want := []cardColorMap{
		colorBGR, colorSwap16, colorBGRSwap16, colorHue, colorOil,
		colorFlux, colorFoil, colorTint, colorVivid, colorRGB,
	}
	for i, w := range want {
		click()
		if c.colorMap != w {
			t.Fatalf("click %d: mapping = %d, want %d", i, c.colorMap, w)
		}
	}

	// The cycle visibly changes the frame for every non-identity mapping.
	c.colorMap = colorBGR
	c.render(p)
	if framesEqual(base, p.frame) {
		t.Fatal("BGR mapping did not change the rendered frame")
	}
}
