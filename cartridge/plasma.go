package cartridge

import "math"

// Plasma is a port of the reference firmware's showcase/carts/plasma. The
// hue table and the plasma field are both built once in Start; Update nudges
// the field by two every frame and maps it through the hue table, rewriting
// every pixel. No assets, no input, no machine.
type Plasma struct {
	hue   [256]uint16
	field [Width * Height]uint8
}

// NewPlasma returns the plasma cartridge.
func NewPlasma() Cartridge { return &Plasma{} }

// Name implements Cartridge.
func (c *Plasma) Name() string { return "PLASMA" }

// Start rebuilds the hue table and plasma field. Because the runtime always
// constructs a fresh Plasma before calling Start, this is the whole state.
func (c *Plasma) Start(p *Platform) {
	for i := 0; i < 256; i++ {
		r, g, b := hsv2rgb(float64(i)*360.0/256.0, 1, 1)
		r5 := uint16(r * 31)
		g6 := uint16(g * 63)
		b5 := uint16(b * 31)
		c.hue[i] = r5<<11 | g6<<5 | b5
	}

	for y := 0; y < Height; y++ {
		fy := float64(y)
		for x := 0; x < Width; x++ {
			fx := float64(x)
			v := math.Sin(fx/16) +
				math.Sin(fy/8) +
				math.Sin((fx+fy)/16) +
				math.Sin(math.Sqrt(fx*fx+fy*fy)/8)
			v = (v + 4) * 32
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			c.field[y*Width+x] = uint8(v)
		}
	}
}

// Update advances the field and repaints the full frame.
func (c *Plasma) Update(p *Platform) {
	fb := p.Frame()
	for i := range fb {
		c.field[i] += 2
		fb[i] = c.hue[c.field[i]]
	}
}

// hsv2rgb mirrors the reference implementation: h in degrees, s and v in
// 0..1, with hue wrapped by mod 2.
func hsv2rgb(h, s, v float64) (float64, float64, float64) {
	c := v * s
	hh := h / 60.0
	x := c * (1 - math.Abs(hh-2*math.Floor(hh/2)-1))
	m := v - c

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return r + m, g + m, b + m
}
