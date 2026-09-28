package cartridge

// Ported from showcase/carts/zeroman/src/Enemy.zig. Only the gopher exists in
// the needleman stage.

type enemyType uint8

const enemyGopher enemyType = 0

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
	case enemyGopher:
		e.tickGopher(z, attribs)
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
	case enemyGopher:
		e.drawGopher(z)
	}
}

func (e *enemy) tickGopher(z *zeroman, attribs []tileAttrib) {
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

func (e *enemy) drawGopher(z *zeroman) {
	if e.health == 0 {
		drawDeathEffectSmall(z, e.box.x+e.box.w/2, e.box.y+e.box.h/2, e.deathFrames)
		return
	}
	if e.invincibility%6 >= 3 {
		z.drawSprite(&gfxHurt, 0, 0, gfxHurt.w, gfxHurt.h, false, e.box.x-4, e.box.y)
		return
	}
	z.drawSprite(&gfxGopher, int(e.frame)*24, 0, 24, 24, e.faceLeft, e.box.x-4, e.box.y)
}
