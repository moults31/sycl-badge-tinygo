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
// the LCD. It samples the baked glow mask, breathes it, sweeps a holo band
// across it, adds a type-tinted ambient wash and drifting sparkles, and fires
// a flash on A. Left/Right cycle the baked library; Select toggles a
// calibration overlay (joystick nudges the mask, A/B rotate it) for lining the
// glow up with the printed art.
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
)

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

	c.list = c.library
	if len(c.list) == 0 {
		c.list = cards
	}
	c.cals = make([]cardCalib, len(c.list))

	for i := 0; i < 256; i++ {
		c.sin[i] = uint8((math.Sin(float64(i)*2*math.Pi/256) + 1) * 127.5)
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

	if pressed(p.buttons.Select, p.prev.Select) {
		c.calib = !c.calib
	}
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
		// Cycle the loaded library.
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

	// Breathing envelope 0..255, ~3 s at 60 fps.
	breath := int(c.sin[(int(c.t)*256/180)&255])
	bf := 170 + breath*85/255

	// Attack flash, 0..255.
	fl := int(c.flash) * 255 / cardFlashFrames

	// Global intensity breathing through the backlight PWM; the flash pins it
	// to full. Per-pixel breathing (bf) is softer so the two don't compound
	// into an obvious square wave.
	bl := 150 + breath*105/255
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
			if idx > 48 {
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

	c.drawSparks(fb, bf)

	if c.calib {
		c.drawGuides(p)
	}
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
// per-pixel breathing, the diagonal holo band, and the attack-flash lift --
// applied to the color channels directly (RGB565 channel ranges: 5/6/5 bits).
func (c *CardShow) artPixel(x, y, bf, hy, holo, fl int) uint16 {
	r00, g00, b00 := c.artAt(x, y)

	// Breathing dims the whole art (same soft envelope as the mask mode).
	rr := int(r00) * bf / 255
	gg := int(g00) * bf / 255
	bb := int(b00) * bf / 255

	// Diagonal holo band, subtle brightness sweep across the art.
	d := (int(c.sin[(x*3+hy+holo)&255]) - 128) / 24
	rr += d
	gg += d
	bb += d

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
