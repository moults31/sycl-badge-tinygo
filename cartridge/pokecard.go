package cartridge

import (
	"fmt"
	"math"
)

// CardAsset is one baked card lightshow asset, produced by tools/make_card.py.
//
// The mask is a coarse 4-bit "glow" map (luminance of the card's art box,
// softened and quantized), packed two nibbles per byte, row-major, high nibble
// first. The palette runs dark -> bright and is interpolated into a 256-entry
// ramp at Start. Because light diffuses through the card stock, the mask is
// deliberately small and is bilinearly upscaled on device.
// Ambient is the very dim RGB wash the background (mask==0) settles at; the
// generator derives it from the card's signature color so the unlit card reads
// as "off" instead of a lifted luminance photo.
//
// Art-mode assets carry Art/ArtW/ArtH instead: a full-vibrance RGB565 image of
// the card's art box, packed little-endian as a Go string so the compiler
// keeps it in flash rodata (no RAM copy at boot). The runtime renders it
// directly, playing the animation effects (breathing, holo band, flash,
// sparkles) on top. Mask and Palette are then unused.
type CardAsset struct {
	Name    string
	Set     string
	Types   []string
	Rarity  string
	MaskW   int
	MaskH   int
	Mask    []byte
	Palette []uint16
	Ambient uint16
	ArtW    int
	ArtH    int
	Art     string
}

// artMode reports whether this asset renders from the baked art image.
func (a *CardAsset) artMode() bool { return len(a.Art) > 0 }

// artPx returns the RGB565 pixel at index i of the packed art (little-endian).
func (a *CardAsset) artPx(i int) uint16 {
	s := a.Art
	return uint16(s[2*i]) | uint16(s[2*i+1])<<8
}

// nib returns the 4-bit mask value at (x, y); coords must be in range.
func (a *CardAsset) nib(x, y int) int {
	i := y*a.MaskW + x
	b := a.Mask[i>>1]
	if i&1 == 0 {
		return int(b >> 4)
	}
	return int(b & 0x0F)
}

// cardCalib is the per-card placement trim (LCD pixels / shear units).
type cardCalib struct {
	offX, offY, rot int
}

// CardShow runs the "shinethrough" lightshow behind the physical card laid on
// the LCD. It samples the baked glow mask, animates it with independent effect
// layers (breathing, a diagonal holo band, drifting sparkles, an A-button
// flash), and maps the finished frame through a colour look. Left/Right cycle
// the baked library; Select toggles the alignment overlay (joystick nudges the
// mask, A/B rotate it) for lining the glow up with the printed art; Click
// cycles the output colour mapping; B toggles breathing; Start opens the
// effects menu.
type CardShow struct {
	card CardAsset
	cal  *cardCalib

	// library is an optional runtime-supplied card set (used by the host
	// simulator, which loads cards from the user's local folder at startup).
	// When empty, Start falls back to the baked `cards` the firmware ships.
	library []CardAsset
	// list is the set resolved for this launch: library if set, else cards.
	list []CardAsset

	sin  [256]uint8 // sin, biased to 0..255
	ramp [256]uint16

	t     uint32
	flash uint32
	spks  []spark

	// Effect toggles. They are independent: each is a layer that can be on or
	// off, and any combination composes (the strobe from BREATHE+HOLO is a
	// feature, not an accident).
	//
	//   - breathe  the ~3 s brightness envelope (per-pixel and backlight PWM);
	//   - holo     the diagonal brightness sweep;
	//   - sparkle  the drifting white glints.
	//
	// Click cycles the colour mapping, B toggles BREATHE, and the menu (A held)
	// edits all of them.
	breathe bool
	holo    bool
	sparkle bool

	// menu is true while the effects menu is open; the show keeps rendering
	// behind it so edits preview live.
	menu bool
	// menuRow is the focused row (see the cardMenuRow constants).
	menuRow int

	// colorMap selects how the finished frame is mapped onto the panel's
	// colour order; Click cycles it (see the cardColorMap constants).
	colorMap cardColorMap
	// mapToast counts down the frames the mapping name stays on screen after
	// the user cycles it.
	mapToast uint32

	// plasma is the animated 0..255 field (the same generator as the PLASMA
	// cart) that drives the FLUX/FOIL/TINT modes. hueM holds a composed
	// chroma-rotation matrix per field value for FLUX; plasmaHue is the
	// full-saturation hue table FOIL and TINT sample.
	plasma    []uint8
	hueM      [256][3][3]int32
	plasmaHue [256]uint16

	idx   int
	calib bool
	cals  []cardCalib
}

type spark struct {
	x, y  int
	phase uint8
	speed uint8
	rad   uint8
}

const (
	cardFlashFrames = 24
	cardSpkCount    = 8

	// cardMapToastFrames is how long the colour-mapping name lingers after a
	// click cycle (~1.5 s at 60 fps).
	cardMapToastFrames = 90

	// cardHuePeriodFrames is one full turn of the animated HUE mapping (~3 s
	// at 60 fps). NOTE: this now matches the ~3 s breathing envelope, so the
	// two can beat against each other when BREATHE and HUE are both on; bump
	// this (or the breath rate) if that becomes distracting.
	cardHuePeriodFrames = 180

	// cardOilRotate is the OIL mapping's bit rotation. swap16 is a full
	// byte swap (rotate 8); a smaller rotation keeps more of the high-order
	// edge bits in the high-order channels, so the silhouette survives.
	cardOilRotate = 6

	// cardVividSat is VIVID's saturation gain, in percent (170 = 1.7x).
	cardVividSat = 170

	// cardFoilGain scales FOIL's plasma sheen (of 255).
	cardFoilGain = 140
)

// cardColorMap selects how CARD SHOW's finished RGB565 frame is mapped onto the
// panel's colour order.
//
// The runtime renders standard RGB565 (R in bits 15..11), which is what the
// host simulator decodes. The badge's DT018BTFT panel is wired BGR, and
// display.go sets MADCTL's BGR bit so the two agree -- so colorRGB is correct
// on both.
//
// The rest are a runtime cycle (joystick Click). The first four are panel-order
// fallbacks and bit-rotation look modes; HUE and the four plasma modes move
// only the chroma, leaving luminance (and therefore the artwork's edges)
// intact:
//
//   - HUE   -- a global luma-preserving hue rotation that spins with time.
//   - FLUX  -- the plasma field rotates each pixel's own hue; saturation and
//     luminance stay the art's, so it floods with moving colour but keeps lines.
//   - FOIL  -- a plasma tint screen-blended by luminance: a holo sheen.
//   - TINT  -- the art multiplied by a plasma colour: an animated gel.
//   - VIVID -- a static saturation boost, no recolour.
type cardColorMap uint8

const (
	colorRGB       cardColorMap = iota // as baked; correct with MADCTL BGR set
	colorBGR                           // swap red/blue: the pre-BGR-fix appearance
	colorSwap16                        // swap the pixel's two bytes (endianness)
	colorBGRSwap16                     // both
	colorHue                           // animated luma-preserving hue rotation
	colorOil                           // bit-rotate 6: swap16's silhouette-safe cousin
	colorFlux                          // plasma rotates the art's own hue
	colorFoil                          // plasma-tinted holo sheen
	colorTint                          // plasma colour-gel multiply
	colorVivid                         // saturation/vibrancy boost
	colorMapCount
)

// cardColorNames labels the mapping on the toast shown after a click cycle.
var cardColorNames = [...]string{
	"RGB", "BGR", "SWAP16", "BGR+SWAP16", "HUE", "OIL",
	"FLUX", "FOIL", "TINT", "VIVID",
}

// cardMenuRow identifies a row of the effects menu. Rows COLOUR and CARD are
// single-select (Left/Right cycles the value); BREATHE, HOLO and SPARKLE are
// booleans (Left/Right or A toggles them).
type cardMenuRow uint8

const (
	rowColour cardMenuRow = iota
	rowCard
	rowBreathe
	rowHolo
	rowSparkle
	cardMenuRows // count
)

// cardMenuNames labels each row in the menu.
var cardMenuNames = [...]string{"COLOUR", "CARD", "BREATHE", "HOLO", "SPARKLE"}

// NewCardShow returns the card lightshow cartridge using the baked library.
func NewCardShow() Cartridge { return &CardShow{} }

// NewCardShowWith returns the card lightshow cartridge driven by an explicit
// card library instead of the baked one. The host simulator uses it to render
// the cards found in the user's local folder; the firmware always uses
// NewCardShow and the baked cards.
func NewCardShowWith(library []CardAsset) Cartridge { return &CardShow{library: library} }

// Name implements Cartridge.
func (c *CardShow) Name() string { return "CARD SHOW" }

// Start loads the baked library and rebuilds all per-launch state.
func (c *CardShow) Start(p *Platform) {
	c.t, c.flash = 0, 0
	c.idx = 0
	c.calib = false
	c.colorMap = colorRGB
	c.mapToast = 0
	c.breathe = true
	c.holo = true
	c.sparkle = true
	c.menu = false
	c.menuRow = 0

	c.list = c.library
	if len(c.list) == 0 {
		c.list = cards
	}
	c.cals = make([]cardCalib, len(c.list))

	for i := 0; i < 256; i++ {
		c.sin[i] = uint8((math.Sin(float64(i)*2*math.Pi/256) + 1) * 127.5)
	}

	// Plasma-driven modes: the field is the PLASMA cart's generator, built
	// once and advanced per frame. hueM[field] is the chroma-rotation matrix
	// with that field value as the angle (for FLUX); plasmaHue[field] is the
	// full-saturation colour it maps to (for FOIL/TINT).
	c.plasma = buildPlasmaField()
	c.hueM = buildHueMatrices(c.sin)
	for i := 0; i < 256; i++ {
		r, g, b := hsv2rgb(float64(i)*360.0/256.0, 1, 1)
		c.plasmaHue[i] = RGB565(uint8(r*255), uint8(g*255), uint8(b*255))
	}

	if len(c.list) > 0 {
		c.loadCard()
	}
}

// loadCard (re)builds the per-card ramp and sparkle field for cards[idx].
func (c *CardShow) loadCard() {
	c.card = c.list[c.idx]
	c.cal = &c.cals[c.idx]
	c.buildRamp()

	// Deterministic sparkle field (fixed seed so frames hash-stable).
	c.spks = make([]spark, cardSpkCount)
	seed := uint32(0x5EED1234)
	next := func() uint32 {
		seed = seed*1664525 + 1013904223
		return seed
	}
	for i := range c.spks {
		c.spks[i] = spark{
			x:     int(next() % Width),
			y:     int(next() % Height),
			phase: uint8(next()),
			speed: uint8(1 + next()%3),
			rad:   uint8(2 + next()%3),
		}
	}
}

// buildRamp interpolates the palette into a 256-entry dark->bright RGB565 ramp.
func (c *CardShow) buildRamp() {
	n := len(c.card.Palette)
	if n < 2 {
		return
	}
	for i := 0; i < 256; i++ {
		pos := float64(i) / 255.0 * float64(n-1)
		j := int(pos)
		if j > n-2 {
			j = n - 2
		}
		if j < 0 {
			j = 0
		}
		f := pos - float64(j)
		r0, g0, b0 := unpack(c.card.Palette[j])
		r1, g1, b1 := unpack(c.card.Palette[j+1])
		r := uint8(float64(r0) + (float64(r1)-float64(r0))*f)
		g := uint8(float64(g0) + (float64(g1)-float64(g0))*f)
		b := uint8(float64(b0) + (float64(b1)-float64(b0))*f)
		c.ramp[i] = RGB565(r, g, b)
	}
}

// Update implements Cartridge.
func (c *CardShow) Update(p *Platform) {
	c.t++

	// Advance the plasma field by two, exactly like the PLASMA cart, so the
	// plasma-driven colour modes move at the plasma's own rate.
	for i := range c.plasma {
		c.plasma[i] += 2
	}

	if c.menu {
		c.updateMenu(p)
		c.render(p)
		if c.flash > 0 {
			c.flash--
		}
		if c.mapToast > 0 {
			c.mapToast--
		}
		return
	}

	if pressed(p.buttons.Select, p.prev.Select) {
		c.calib = !c.calib
	}
	// Joystick click cycles the output colour mapping. It is unused elsewhere,
	// so it works in both the lightshow and the calibration overlay.
	if pressed(p.buttons.Click, p.prev.Click) {
		c.colorMap = (c.colorMap + 1) % colorMapCount
		c.mapToast = cardMapToastFrames
	}
	// B toggles the breathing envelope; it works in the lightshow and the
	// calibration overlay both.
	if pressed(p.buttons.B, p.prev.B) && !c.calib {
		c.breathe = !c.breathe
		c.mapToast = cardMapToastFrames
	}

	// Start opens the effects menu. Start+Select is the runtime's exit chord,
	// so the menu only opens when Start is pressed alone (Select up) and not
	// while the calibration overlay is up.
	if pressed(p.buttons.Start, p.prev.Start) && !p.buttons.Select && !c.calib {
		c.menu = true
		c.menuRow = 0
		c.flash = 0
	}

	// A is the attack flash.
	if pressed(p.buttons.A, p.prev.A) && !c.calib {
		c.flash = cardFlashFrames
	}

	if c.calib {
		// Held directions nudge continuously; A/B step the rotation.
		if p.buttons.Left {
			c.cal.offX--
		}
		if p.buttons.Right {
			c.cal.offX++
		}
		if p.buttons.Up {
			c.cal.offY--
		}
		if p.buttons.Down {
			c.cal.offY++
		}
		if pressed(p.buttons.A, p.prev.A) {
			c.cal.rot--
		}
		if pressed(p.buttons.B, p.prev.B) {
			c.cal.rot++
		}
		c.cal.offX = clampInt(c.cal.offX, -48, 48)
		c.cal.offY = clampInt(c.cal.offY, -48, 48)
		c.cal.rot = clampInt(c.cal.rot, -8, 8)
	} else {
		// Cycle the loaded library. The menu handles its own card row.
		prev := c.idx
		if pressed(p.buttons.Left, p.prev.Left) && len(c.list) > 0 {
			c.idx = (c.idx - 1 + len(c.list)) % len(c.list)
		}
		if pressed(p.buttons.Right, p.prev.Right) && len(c.list) > 0 {
			c.idx = (c.idx + 1) % len(c.list)
		}
		if c.idx != prev {
			c.flash = 0
			c.loadCard()
		}
	}

	c.render(p)

	if c.flash > 0 {
		c.flash--
	}
	if c.mapToast > 0 {
		c.mapToast--
	}
}

// updateMenu handles input while the effects menu is open. Up/Down move the
// focused row; Left/Right change its value; A toggles a boolean row or the
// selected row's value; B closes. Card and colour rows wrap.
func (c *CardShow) updateMenu(p *Platform) {
	row := cardMenuRow(c.menuRow)
	if pressed(p.buttons.Up, p.prev.Up) && c.menuRow > 0 {
		c.menuRow--
	}
	if pressed(p.buttons.Down, p.prev.Down) && c.menuRow < int(cardMenuRows)-1 {
		c.menuRow++
	}

	dec, inc, toggle := false, false, false
	if pressed(p.buttons.Left, p.prev.Left) {
		dec = true
	}
	if pressed(p.buttons.Right, p.prev.Right) {
		inc = true
	}
	if pressed(p.buttons.A, p.prev.A) {
		toggle = true
	}

	switch row {
	case rowColour:
		if dec {
			c.colorMap = (c.colorMap - 1 + colorMapCount) % colorMapCount
		}
		if inc {
			c.colorMap = (c.colorMap + 1) % colorMapCount
		}
	case rowCard:
		if len(c.list) > 0 && (dec || inc) {
			if dec {
				c.idx = (c.idx - 1 + len(c.list)) % len(c.list)
			} else {
				c.idx = (c.idx + 1) % len(c.list)
			}
			c.flash = 0
			c.loadCard()
		}
	case rowBreathe:
		if dec || inc || toggle {
			c.breathe = !c.breathe
		}
	case rowHolo:
		if dec || inc || toggle {
			c.holo = !c.holo
		}
	case rowSparkle:
		if dec || inc || toggle {
			c.sparkle = !c.sparkle
		}
	}

	if pressed(p.buttons.B, p.prev.B) || pressed(p.buttons.Start, p.prev.Start) {
		c.menu = false
	}
}

// render composites the show into the frame.
func (c *CardShow) render(p *Platform) {
	fb := p.Frame()
	for i := range fb {
		fb[i] = 0
	}
	if len(c.list) == 0 {
		return
	}

	// Breathing envelope 0..255, ~3 s at 60 fps. When toggled off, bf is a
	// flat 255 (full-bright art) and the backlight holds steady.
	breath := int(c.sin[(int(c.t)*256/180)&255])
	bf := 170 + breath*85/255

	// Attack flash, 0..255.
	fl := int(c.flash) * 255 / cardFlashFrames

	// Global intensity breathing through the backlight PWM; the flash pins it
	// to full. Per-pixel breathing (bf) is softer so the two don't compound
	// into an obvious square wave.
	//
	// When breathing is off, bf holds at full and the backlight goes to full
	// rather than a fixed intermediate duty: the TPS61041 boost only lights
	// predictably at full duty (an intermediate duty drifts, see the README's
	// backlight notes), so "steady" means "full".
	bl := 150 + breath*105/255
	if !c.breathe {
		bf = 255
		bl = 255
	}
	if fl > 0 {
		bl = 255
	}
	p.Backlight(uint8(bl))

	holo := int(c.t) * 2
	ambR, ambG, ambB := unpack(c.card.Ambient)
	// Background drift field, slow and unrelated to the holo sweep speed so
	// the two layers never move in lockstep.
	drift := int(c.t) * 120
	// Art mode renders the baked full-vibrance image; the effects below are
	// applied to its color channels instead of the glow mask.
	artMode := c.card.artMode()

	for y := 0; y < Height; y++ {
		// Small-angle rotation as a per-row x shear.
		sh := (y - Height/2) * c.cal.rot / 64
		row := y * Width
		hy := y * 2
		wash := int(c.sin[(y*5+drift)&255]) >> 5 // 0..7 ambient wobble
		for x := 0; x < Width; x++ {
			if artMode {
				fb[row+x] = c.artPixel(x+sh-c.cal.offX, y-c.cal.offY,
					bf, hy, holo, fl)
				continue
			}
			a := c.sample(x+sh-c.cal.offX, y-c.cal.offY) // 0..15

			if a == 0 {
				// Background: the dim ambient wash, breathing gently with the
				// global envelope. A lerp toward black by the breath keeps the
				// floor visibly *below* the subject at its dimmest.
				k := 140 + breath*55/255 // 140..195 of 255: keep it clearly dark
				if !c.breathe {
					k = 195
				}
				rr := int(ambR) * k / 255
				gg := int(ambG) * k / 255
				bb := (int(ambB) + wash) * k / 255
				if bb > 63 {
					bb = 63
				}
				if fl > 0 {
					lift := fl / 4
					rr = min8(rr+lift, 63)
					gg = min8(gg+lift, 63)
					bb = min8(bb+lift, 63)
				}
				fb[row+x] = RGB565(uint8(rr), uint8(gg), uint8(bb))
				continue
			}

			idx := a * 17

			// Breathing applies to the glow itself.
			idx = idx * bf / 255

			// Diagonally sweeping holo band. Only tint lit art, not the
			// unlit negative space, and keep it subtle.
			if c.holo && idx > 48 {
				idx += (int(c.sin[(x*3+hy+holo)&255]) - 128) / 20
			}

			// Flash lifts the subject and adds a moving highlight.
			if fl > 0 {
				idx += fl / 3
			}

			idx = clampInt(idx, 0, 255)
			fb[row+x] = c.ramp[idx]
		}
	}

	if c.sparkle {
		c.drawSparks(fb, bf)
	}

	if c.calib {
		c.drawGuides(p)
	}

	if !c.menu && c.mapToast > 0 {
		c.drawMapToast(p)
	}

	// Apply the selected colour mapping to the show, so it covers the sparks
	// and the calibration overlay.
	c.applyColorMap(fb, c.colorMap)

	// The menu is drawn after the colour map, so its text stays legible no
	// matter which look mode is active (the show behind it carries the look).
	if c.menu {
		c.drawMenu(p)
	}
}

// drawMenu overlays the effects menu. The show keeps rendering behind a
// translucent dark panel, so a row edit is visible immediately. Up/Down move
// the focus; Left/Right change the value; A toggles; B closes.
func (c *CardShow) drawMenu(p *Platform) {
	fb := p.Frame()

	// Dim the whole frame so the menu text reads over any look mode.
	for i := range fb {
		r, g, b := unpack(fb[i])
		fb[i] = RGB565(r/3, g/3, b/3)
	}

	bg := RGB565(0x08, 0x0C, 0x18)
	fg := RGB565(0xE0, 0xE8, 0xF0)
	sel := RGB565(0x30, 0x60, 0xC0)
	lit := RGB565(0xFF, 0xFF, 0x00)
	dim := RGB565(0x88, 0x94, 0xA8)

	// Panel: a title bar and one row per menu item. The 8x8 font gives 20
	// columns across the panel; the panel inner width is 18 columns, so the
	// label sits at column 0 and the value at column 11 (7 columns for the
	// longest value, "BGR+SWAP16", which is trimmed to fit).
	const x0, y0 = 4, 6
	const labelCol, valueCol = 0, 10
	p.fillRect(x0, y0, Width-2*x0, 16+int(cardMenuRows)*12, bg)
	p.drawText(x0, y0+3, "CARD SHOW", fg, bg)
	p.fillRect(x0, y0+12, Width-2*x0, 1, sel)

	val := func(r cardMenuRow) string {
		switch r {
		case rowColour:
			return cardColorNames[c.colorMap]
		case rowCard:
			if len(c.list) == 0 {
				return "-"
			}
			name := c.card.Name
			if len(name) > 7 {
				name = name[:7]
			}
			return name
		case rowBreathe:
			return onOff(c.breathe)
		case rowHolo:
			return onOff(c.holo)
		case rowSparkle:
			return onOff(c.sparkle)
		}
		return ""
	}

	for i := 0; i < int(cardMenuRows); i++ {
		y := y0 + 15 + i*12
		r := cardMenuRow(i)
		focused := i == c.menuRow

		label := "  " + cardMenuNames[i]
		v := val(r)
		if len(v) > 7 {
			v = v[:7]
		}

		if focused {
			p.fillRect(x0, y, Width-2*x0, 10, sel)
			p.drawText(x0+labelCol*8, y+1, label, lit, sel)
			p.drawText(x0+valueCol*8, y+1, v, lit, sel)
			continue
		}
		p.drawText(x0+labelCol*8, y+1, label, fg, bg)
		p.drawText(x0+valueCol*8, y+1, v, dim, bg)
	}

	p.drawText(x0, y0+15+int(cardMenuRows)*12+2, "UP/DN ROW  B CLOSES", dim, bg)
}

// onOff renders a boolean menu value.
func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

// drawMapToast briefly names the active colour mapping and breathing state
// after a click cycle or a B toggle.
func (c *CardShow) drawMapToast(p *Platform) {
	name := "CMAP " + cardColorNames[c.colorMap]
	if !c.breathe {
		name += " NO-BREATHE"
	}
	p.drawText(3, 3, name, RGB565(0xFF, 0xFF, 0x00), RGB565(0x00, 0x00, 0x00))
}

// applyColorMap rewrites every pixel of a finished frame through the selected
// mapping. The identity case is a no-op. t is the frame counter, used only by
// the animated mappings (HUE and the plasma-driven ones).
func (c *CardShow) applyColorMap(fb []uint16, m cardColorMap) {
	switch m {
	case colorBGR:
		for i, v := range fb {
			fb[i] = swapRB(v)
		}
	case colorSwap16:
		for i, v := range fb {
			fb[i] = rot16(v, 8)
		}
	case colorBGRSwap16:
		for i, v := range fb {
			fb[i] = rot16(swapRB(v), 8)
		}
	case colorOil:
		for i, v := range fb {
			fb[i] = rot16(v, cardOilRotate)
		}
	case colorHue:
		applyHueRotate(fb, c.t)
	case colorFlux:
		c.applyFlux(fb)
	case colorFoil:
		c.applyFoil(fb)
	case colorTint:
		c.applyTint(fb)
	case colorVivid:
		applyVivid(fb)
	}
}

// rot16 rotates a 16-bit pixel left by k bits. swap16 is rot16(v, 8); OIL uses
// a smaller rotation so more high-order (edge) bits stay in the high-order
// channels and the silhouette survives the shimmer.
func rot16(v uint16, k uint) uint16 {
	return v<<k | v>>(16-k)
}

// --- plasma-driven colour modes -------------------------------------------

// hueMatrices are the fixed RGB -> YCbCr -> rotate -> RGB basis matrices,
// scaled by 256. mul3 composes out * rotate(angle) * in so a mapping needs one
// composed matrix per angle instead of a per-pixel conversion.
var (
	hueIn = [3][3]float64{
		{0.299, 0.587, 0.114},
		{-0.168736, -0.331264, 0.5},
		{0.5, -0.418688, -0.081312},
	}
	hueOut = [3][3]float64{
		{1, 0, 1.402},
		{1, -0.344136, -0.714136},
		{1, 1.772, 0},
	}
)

// buildHueMatrices precomputes the composed colour-rotation matrix for a full
// turn: index i is the angle whose sin is derived from sin[] (i maps to i/256
// of a turn). FLUX indexes these by the plasma field value directly.
func buildHueMatrices(sin [256]uint8) [256][3][3]int32 {
	var m [256][3][3]int32
	for i := 0; i < 256; i++ {
		s := (float64(sin[(i+64)&255]) - 127.5) / 127.5 // sin of the angle
		c := (float64(sin[i]) - 127.5) / 127.5          // cos of the angle
		rot := [3][3]float64{
			{1, 0, 0},
			{0, c, -s},
			{0, s, c},
		}
		mm := mul3(hueOut, mul3(rot, hueIn))
		for r := 0; r < 3; r++ {
			for col := 0; col < 3; col++ {
				m[i][r][col] = int32(math.Round(mm[r][col] * 256))
			}
		}
	}
	return m
}

// buildHueMatrix composes the single matrix for an angle of turns*2pi.
func buildHueMatrix(turns float64) [3][3]int32 {
	a := 2 * math.Pi * turns
	c, s := math.Cos(a), math.Sin(a)
	rot := [3][3]float64{
		{1, 0, 0},
		{0, c, -s},
		{0, s, c},
	}
	mm := mul3(hueOut, mul3(rot, hueIn))
	var m [3][3]int32
	for r := 0; r < 3; r++ {
		for col := 0; col < 3; col++ {
			m[r][col] = int32(math.Round(mm[r][col] * 256))
		}
	}
	return m
}

// applyHueRotate rotates the chroma of every pixel by a slowly advancing angle
// while leaving its luminance untouched (RGB -> YCbCr, rotate (Cb,Cr), back).
// Because Y carries the edges, the shape is preserved exactly; only the hues
// move, so the whole frame slowly rainbows.
func applyHueRotate(fb []uint16, t uint32) {
	m := buildHueMatrix(float64(t%cardHuePeriodFrames) / cardHuePeriodFrames)
	applyMatrix(fb, m)
}

// applyMatrix runs every pixel through a composed 3x3 colour matrix (scaled by
// 256). One integer dot product per output channel.
func applyMatrix(fb []uint16, m [3][3]int32) {
	for i, v := range fb {
		r, g, b := unpackInt(v)
		fb[i] = RGB565(
			clamp8(int((r*m[0][0]+g*m[0][1]+b*m[0][2]+128)>>8)),
			clamp8(int((r*m[1][0]+g*m[1][1]+b*m[1][2]+128)>>8)),
			clamp8(int((r*m[2][0]+g*m[2][1]+b*m[2][2]+128)>>8)),
		)
	}
}

// mul3 multiplies two 3x3 matrices.
func mul3(a, b [3][3]float64) [3][3]float64 {
	var r [3][3]float64
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			r[i][j] = a[i][0]*b[0][j] + a[i][1]*b[1][j] + a[i][2]*b[2][j]
		}
	}
	return r
}

// unpackInt splits an RGB565 pixel into int32 8-bit components for the colour
// matrix math.
func unpackInt(v uint16) (int32, int32, int32) {
	r := int32((v >> 11) & 0x1F)
	g := int32((v >> 5) & 0x3F)
	b := int32(v & 0x1F)
	return r << 3, g << 2, b << 3
}

// applyFlux rotates each pixel's hue by the plasma field at that point, but
// leaves its saturation and luminance to the art. Luminance is untouched, so
// every line and shadow survives while the colour floods and moves.
func (c *CardShow) applyFlux(fb []uint16) {
	for i, v := range fb {
		m := &c.hueM[c.plasma[i]]
		r, g, b := unpackInt(v)
		fb[i] = RGB565(
			clamp8(int((r*m[0][0]+g*m[0][1]+b*m[0][2]+128)>>8)),
			clamp8(int((r*m[1][0]+g*m[1][1]+b*m[1][2]+128)>>8)),
			clamp8(int((r*m[2][0]+g*m[2][1]+b*m[2][2]+128)>>8)),
		)
	}
}

// applyFoil screen-blends a full-saturation plasma colour over the frame,
// weighted by each pixel's own luminance: bright areas catch more of the sheen,
// dark lines stay dark.
func (c *CardShow) applyFoil(fb []uint16) {
	for i, v := range fb {
		r, g, b := unpackInt(v)
		y := (299*r + 587*g + 114*b) / 1000
		pr, pg, pb := unpackInt(c.plasmaHue[c.plasma[i]])
		k := y * cardFoilGain / 255 / 255
		fb[i] = RGB565(clamp8(int(r+(255-r)*pr*k/255)),
			clamp8(int(g+(255-g)*pg*k/255)),
			clamp8(int(b+(255-b)*pb*k/255)))
	}
}

// applyTint multiplies the art by a plasma colour -- an animated colour gel.
// Multiplicative, so the art's structure is preserved as modulation.
func (c *CardShow) applyTint(fb []uint16) {
	for i, v := range fb {
		r, g, b := unpackInt(v)
		pr, pg, pb := unpackInt(c.plasmaHue[c.plasma[i]])
		fb[i] = RGB565(clamp8(int(r*pr/255)), clamp8(int(g*pg/255)), clamp8(int(b*pb/255)))
	}
}

// applyVivid pumps saturation (and a little contrast) without recolouring, so
// the palette brightens but luminance and lines are unchanged.
func applyVivid(fb []uint16) {
	for i, v := range fb {
		r, g, b := unpackInt(v)
		l := (299*r + 587*g + 114*b) / 1000
		f := func(c int32) uint8 { return clamp8(int(l + (c-l)*cardVividSat/100)) }
		fb[i] = RGB565(f(r), f(g), f(b))
	}
}

// swapRB swaps the 5-bit red and blue fields of a standard RGB565 pixel,
// turning an RGB mapping into BGR and vice versa.
func swapRB(v uint16) uint16 {
	return (v&0x001F)<<11 | (v & 0x07E0) | (v&0xF800)>>11
}

// sample bilinearly reads the mask at LCD coords (x, y), stretching the mask
// across the full panel; out-of-range coords clamp to the edge.
func (c *CardShow) sample(x, y int) int {
	mx := (x * c.card.MaskW << 8) / Width
	my := (y * c.card.MaskH << 8) / Height

	ix, ax := mx>>8, mx&0xFF
	iy, ay := my>>8, my&0xFF

	x0 := clampInt(ix, 0, c.card.MaskW-1)
	x1 := clampInt(ix+1, 0, c.card.MaskW-1)
	y0 := clampInt(iy, 0, c.card.MaskH-1)
	y1 := clampInt(iy+1, 0, c.card.MaskH-1)

	m00 := c.card.nib(x0, y0)
	m10 := c.card.nib(x1, y0)
	m01 := c.card.nib(x0, y1)
	m11 := c.card.nib(x1, y1)

	v := m00*(256-ax)*(256-ay) +
		m10*ax*(256-ay) +
		m01*(256-ax)*ay +
		m11*ax*ay
	return v >> 16
}

// artPixel renders one pixel of an art-mode card: the baked full-vibrance art
// color, with the same animation effects the mask mode applies to the glow --
// per-pixel breathing, the diagonal holo band (when enabled), and the
// attack-flash lift -- applied to the color channels directly (RGB565 channel
// ranges: 5/6/5 bits).
func (c *CardShow) artPixel(x, y, bf, hy, holo, fl int) uint16 {
	r00, g00, b00 := c.artAt(x, y)

	// Breathing dims the whole art (same soft envelope as the mask mode).
	rr := int(r00) * bf / 255
	gg := int(g00) * bf / 255
	bb := int(b00) * bf / 255

	// Diagonal holo band, subtle brightness sweep across the art.
	if c.holo {
		d := (int(c.sin[(x*3+hy+holo)&255]) - 128) / 24
		rr += d
		gg += d
		bb += d
	}

	// Attack flash lifts everything toward white, like the mask mode.
	if fl > 0 {
		lift := fl / 3
		rr += lift
		gg += lift
		bb += lift
	}

	return RGB565(clamp8(rr), clamp8(gg), clamp8(bb))
}

// clamp8 clamps to the 0..255 byte range RGB565 expects.
func clamp8(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// artAt bilinearly reads the baked art at panel coords (x, y), stretching it
// across the full panel; out-of-range coords clamp to the edge.
func (c *CardShow) artAt(x, y int) (uint8, uint8, uint8) {
	mx := (x * c.card.ArtW << 8) / Width
	my := (y * c.card.ArtH << 8) / Height

	ix, ax := mx>>8, mx&0xFF
	iy, ay := my>>8, my&0xFF

	x0 := clampInt(ix, 0, c.card.ArtW-1)
	x1 := clampInt(ix+1, 0, c.card.ArtW-1)
	y0 := clampInt(iy, 0, c.card.ArtH-1)
	y1 := clampInt(iy+1, 0, c.card.ArtH-1)

	r0, g0, b0 := unpack(c.card.artPx(y0*c.card.ArtW + x0))
	r1, g1, b1 := unpack(c.card.artPx(y0*c.card.ArtW + x1))
	r2, g2, b2 := unpack(c.card.artPx(y1*c.card.ArtW + x0))
	r3, g3, b3 := unpack(c.card.artPx(y1*c.card.ArtW + x1))

	mix := func(a, b int) int { return (a*(256-ax) + b*ax) >> 8 }
	hr, hg, hb := mix(int(r0), int(r1)), mix(int(g0), int(g1)), mix(int(b0), int(b1))
	lr, lg, lb := mix(int(r2), int(r3)), mix(int(g2), int(g3)), mix(int(b2), int(b3))

	wy := 256 - ay
	return uint8((hr*wy + lr*ay) >> 8),
		uint8((hg*wy + lg*ay) >> 8),
		uint8((hb*wy + lb*ay) >> 8)
}

// drawSparks adds slow upward-drifting, twinkling glints over the show.
func (c *CardShow) drawSparks(fb []uint16, bf int) {
	for i := range c.spks {
		s := &c.spks[i]
		tw := int(c.sin[(int(s.phase)+int(c.t)*int(s.speed))&255])
		if tw < 32 {
			continue
		}
		sy := ((s.y - int(c.t)*int(s.speed)/3) % Height)
		if sy < 0 {
			sy += Height
		}
		r := int(s.rad)
		rad2 := r*r + 1
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				d2 := dx*dx + dy*dy
				if d2 > r*r {
					continue
				}
				w := (255 - d2*255/rad2) * tw / 255
				w = w * 170 / 255
				blend(fb, s.x+dx, sy+dy, 255, 255, 255, uint8(w))
			}
		}
	}
}

// drawGuides overlays the registration aids used to align the glow with the
// printed art: a border, center crosshair, rotation tick, and the card name.
func (c *CardShow) drawGuides(p *Platform) {
	fb := p.Frame()
	dim := RGB565(0xFF, 0x40, 0x80)

	for x := 0; x < Width; x++ {
		fb[x] = dim
		fb[(Height-1)*Width+x] = dim
	}
	for y := 0; y < Height; y++ {
		fb[y*Width] = dim
		fb[y*Width+Width-1] = dim
	}

	cx, cy := Width/2+c.cal.offX, Height/2+c.cal.offY
	for d := -8; d <= 8; d++ {
		blend(fb, cx+d, cy, 255, 255, 255, 255)
		blend(fb, cx, cy+d, 255, 255, 255, 255)
	}
	// Rotation indicator ticks along the top edge.
	tx := cx + c.cal.rot*10
	for d := 0; d < 4; d++ {
		blend(fb, tx, 2+d, 0x40, 0xFF, 0xFF, 255)
	}

	name := c.card.Name
	if len(name) > 19 {
		name = name[:19]
	}
	if len(c.list) > 1 {
		// "3/5" library index, so cycling is legible in the overlay.
		idx := fmt.Sprintf(" %d/%d", c.idx+1, len(c.list))
		if len(name)+len(idx) > 19 {
			name = name[:19-len(idx)]
		}
		name += idx
	}
	p.drawText(3, Height-10, name, RGB565(0xFF, 0xFF, 0xFF), RGB565(0x10, 0x10, 0x10))
}

// blend lerps a frame pixel (RGB565) toward (r,g,b) by weight w.
func blend(fb []uint16, x, y int, r, g, b, w uint8) {
	if x < 0 || y < 0 || x >= Width || y >= Height {
		return
	}
	i := y*Width + x
	cr, cg, cb := unpack(fb[i])
	iw := 255 - int(w)
	cr = uint8((int(cr)*iw + int(r)*int(w)) / 255)
	cg = uint8((int(cg)*iw + int(g)*int(w)) / 255)
	cb = uint8((int(cb)*iw + int(b)*int(w)) / 255)
	fb[i] = RGB565(cr, cg, cb)
}

// unpack splits an RGB565 pixel into 8-bit components.
func unpack(v uint16) (uint8, uint8, uint8) {
	r := uint8((v >> 11) & 0x1F)
	g := uint8((v >> 5) & 0x3F)
	b := uint8(v & 0x1F)
	return r << 3, g << 2, b << 3
}

// min8 is a tiny min for uint8-range ints (no generics here: Go 1.21 min is
// fine, but the repo keeps to TinyGo-friendly explicit helpers).
func min8(a, b int) int {
	if a < b {
		return a
	}
	return b
}
