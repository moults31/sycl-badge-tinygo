package cartridge

import "math"

// Ported from showcase/carts/zeroman/src/effects.zig.

func drawDeathEffect(z *zeroman, x, y int, counter uint32) {
	sx := int((counter/3)%6) * 24
	for i := 0; i < 8; i++ {
		angle := math.Pi * float64(i) / 4
		r := 2 * float64(counter)
		dx := x + int(math.Cos(angle)*r)
		dy := y + int(math.Sin(angle)*r)
		z.drawSprite(&gfxEffects, sx, 0, 24, 24, false, dx, dy)
	}
}

func drawDeathEffectSmall(z *zeroman, x, y int, counter uint8) {
	frame := int(counter)
	if frame > 4 {
		return
	}
	z.drawSprite(&gfxEffects, frame*24, 0, 24, 24, false, x-12, y-12)
}

func drawTeleportEffect(z *zeroman, playerX, playerY int, counter uint8) {
	if counter < 32 {
		return
	}
	frame := int(counter) - 32
	x := playerX
	y := playerY
	if frame <= 10 || frame == 15 {
		if frame != 15 {
			y -= 16 * (10 - frame)
		}
		for i := 0; i < 4; i++ {
			z.drawSprite(&gfxTeleport, 8, 0, 8, 8, false, x-4, y+i*8-32)
		}
	} else if frame <= 12 {
		z.drawSprite(&gfxTeleport, 0, 16, 24, 16, false, x-12, y-16)
		z.drawSprite(&gfxTeleport, 0, 16, 24, 8, false, x-12, y-24)
		z.drawSprite(&gfxTeleport, 8, 8, 8, 8, false, x-4, y-32)
	} else if frame <= 14 {
		z.drawSprite(&gfxTeleport, 0, 24, 24, 8, false, x-12, y-8)
		z.drawSprite(&gfxTeleport, 8, 8, 8, 8, false, x-4, y-16)
	}
}
