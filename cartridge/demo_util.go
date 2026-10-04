package cartridge

// Numeric drawing helpers shared by the teaching carts. They render through
// drawGlyph rather than drawText so values can be shown every frame without
// building transient strings (which would allocate and, in the HEAP cart,
// perturb the measurement being shown).

// drawUint draws v in decimal, zero-padded to at least digits cells, and
// returns the x just past the last cell.
func drawUint(p *Platform, x, y int, v uint64, digits int, fg, bg uint16) int {
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	for len(buf)-i < digits {
		i--
		buf[i] = '0'
	}
	for k := i; k < len(buf); k++ {
		p.drawGlyph(x, y, buf[k], fg, bg)
		x += 8
	}
	return x
}

// drawKB draws v in whole kilobytes with a trailing K (e.g. 123K), returning
// the x just past the last cell.
func drawKB(p *Platform, x, y int, v uint64, fg, bg uint16) int {
	x = drawUint(p, x, y, v/1024, 1, fg, bg)
	p.drawGlyph(x, y, 'K', fg, bg)
	return x + 8
}

// flashDec decays a frame counter toward zero.
func flashDec(v *uint8) {
	if *v > 0 {
		*v--
	}
}
