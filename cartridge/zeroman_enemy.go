package cartridge

// Ported from showcase/carts/zeroman/src/Enemy.zig. The player is now the
// gopher, so every enemy is an iguana, drawn from the zero sprite sheet.

type enemyType uint8

const enemyIguana enemyType = 0

type enemy struct {
	active        bool
	typ           enemyType
	box           box
	state         uint8
	health        uint8
	invincibility uint8
	deathFrames   uint8
	frame         uint8
	counter       uint32
	faceLeft      bool
}

func (e *enemy) activate(typ enemyType, b box) {
	e.active = true
	e.typ = typ
	e.box = b
	e.health = 2
	e.state = 0
	e.frame = 0
	e.counter = 0
	e.faceLeft = false
}

func (e *enemy) tick(z *zeroman, attribs []tileAttrib) {
	if e.health == 0 {
		e.deathFrames++
		if e.deathFrames == 5 {
			e.active = false
			return
		}
	}
	if e.invincibility > 0 {
		e.invincibility--
	}
	if e.invincibility > 0 {
		return
	}
	switch e.typ {
	case enemyIguana:
		e.tickIguana(z, attribs)
	}
}

func (e *enemy) hurt(damage uint8) {
	if e.invincibility > 0 || e.health == 0 {
		return
	}
	e.health = subSat(e.health, damage)
	e.invincibility = 30
}

func (e *enemy) draw(z *zeroman) {
	switch e.typ {
	case enemyIguana:
		e.drawIguana(z)
	}
}

func (e *enemy) tickIguana(z *zeroman, attribs []tileAttrib) {
	room := &zeromanStage.rooms[z.curRoom]
	switch e.state {
	case 0: // idle
		e.frame = 0
		e.faceLeft = e.counter&16 != 0
		if e.counter == 0 {
			e.counter = z.rng.rangeLessThan(100, 500)
			e.state = 1
		}
	case 1: // walk
		if e.counter&8 != 0 {
			e.frame = 1
		} else {
			e.frame = 2
		}
		amount := 1
		if e.faceLeft {
			amount = -1
		}
		senseX := e.box.x + (e.box.w+amount*e.box.w)/2
		if room.getTileAttribAtPixel(attribs, senseX, e.box.y+e.box.h) != attribSolid ||
			room.clipX(attribs, e.box, amount) != amount {
			e.faceLeft = !e.faceLeft
		} else {
			e.box.x += amount
		}
		if e.counter == 0 {
			e.counter = z.rng.rangeLessThan(100, 200)
			e.state = 0
		}
	}
	if e.counter > 0 {
		e.counter--
	}

	if e.health > 0 && e.box.overlaps(z.player.box) {
		z.player.hurt(4)
	}
}

func (e *enemy) drawIguana(z *zeroman) {
	if e.health == 0 {
		drawDeathEffectSmall(z, e.box.x+e.box.w/2, e.box.y+e.box.h/2, e.deathFrames)
		return
	}
	if e.invincibility%6 >= 3 {
		z.drawSprite(&gfxHurt, 0, 0, gfxHurt.w, gfxHurt.h, false, e.box.x-4, e.box.y)
		return
	}

	// The zero sheet's idle pose is 24 wide; the running poses are 32.
	sx, sw := 0, 24
	if e.state == 1 {
		sx, sw = 48+int(e.frame-1)*32, 32
	}
	const spriteH = 32
	dx := e.box.x + (e.box.w-sw)/2
	dy := e.box.y + e.box.h - spriteH
	z.drawSprite(&gfxZero, sx, 0, sw, spriteH, e.faceLeft, dx, dy)
}
