package cartridge

// Ported from showcase/carts/zeroman/src/Player.zig. Movement is 8.8 fixed
// point, clipped against the room's solid tiles.

const (
	playerWidth     = 16
	playerHeight    = 24
	playerJumpSpeed = -0x04A5
	playerVMax      = 0x0700
	playerMaxHealth = 31
)

type playerState uint8

const (
	psIdle playerState = iota
	psRunning
	psSliding
	psJumping
	psClimbing
	psHurting
)

type playerInput struct{ left, right, up, down, jump, shoot bool }

type shot struct {
	x, y, vx int
	active   bool
}

type player struct {
	box           box
	vx, vy        int
	state         playerState
	faceLeft      bool
	health        uint8
	invincibility uint8
	animTime      uint32
	slideFrames   uint8
	shootFrames   uint8
	shots         [16]shot
}

func (pl *player) reset() {
	*pl = player{
		box:    box{0, 0, playerWidth, playerHeight},
		health: playerMaxHealth,
	}
}

func (pl *player) tick() {
	pl.animTime++
	if pl.invincibility > 0 {
		pl.invincibility--
	}
	if pl.slideFrames > 0 {
		pl.slideFrames--
	}
	if pl.shootFrames > 0 {
		pl.shootFrames--
	}
}

func (pl *player) hurt(damage uint8) {
	if pl.invincibility > 0 {
		return
	}
	pl.health = subSat(pl.health, damage)
	if pl.state != psSliding {
		pl.state = psHurting
	}
	pl.invincibility = 60
}

func (pl *player) draw(z *zeroman) {
	if pl.invincibility%6 >= 3 {
		if pl.state == psHurting {
			z.drawSprite(&gfxHurt, 0, 0, gfxHurt.w, gfxHurt.h, false, pl.box.x-4, pl.box.y)
		}
		return
	}

	// The player is the gopher now: a single 24x24 sheet with two front-facing
	// poses (1, 2) and a back view (0). Idle and the states without dedicated
	// art use pose 1 so the face is visible; running alternates 1 and 2.
	frame := 1
	if pl.state == psRunning {
		if pl.animTime%20 < 10 {
			frame = 1
		} else {
			frame = 2
		}
	}
	const gopherSize = 24
	sx := frame * gopherSize
	dx := pl.box.x + (pl.box.w-gopherSize)/2
	dy := pl.box.y + pl.box.h - gopherSize
	z.drawSprite(&gfxGopher, sx, 0, gopherSize, gopherSize, pl.faceLeft, dx, dy)

	for i := range pl.shots {
		if pl.shots[i].active {
			s := &pl.shots[i]
			z.drawSprite(&gfxShot, 0, 0, gfxShot.w, gfxShot.h, false, s.x-4, s.y-4)
		}
	}
}

func (pl *player) handleInput(room *room, attribs []tileAttrib, input, prev playerInput) {
	switch pl.state {
	case psIdle, psRunning, psJumping:
		pl.doMovement(room, attribs, input, prev)
		pl.doShooting(input, prev)
	case psClimbing:
		pl.doClimbing(room, attribs, input, prev)
		pl.doShooting(input, prev)
	case psSliding:
		pl.doSliding(room, attribs, input, prev)
	case psHurting:
		pl.doHurting(room, attribs)
	}
}

func (pl *player) move(room *room, attribs []tileAttrib) {
	amountX := pl.vx >> 8
	amountY := pl.vy >> 8
	pl.box.x += room.clipX(attribs, pl.box, amountX)
	clippedY := room.clipY(attribs, pl.box, amountY)
	pl.box.y += clippedY
	if clippedY != amountY && pl.vy < 0 {
		pl.vy = 0
	}
	for i := range pl.shots {
		s := &pl.shots[i]
		if !s.active {
			continue
		}
		b := box{s.x - 4, s.y - 3, 8, 6}
		cx := room.clipX(attribs, b, s.vx)
		if cx != s.vx {
			s.active = false
		} else {
			s.x += cx
		}
	}
}

func (pl *player) doShooting(input, prev playerInput) {
	if !input.shoot || prev.shoot {
		return
	}
	pl.shootFrames = 16
	flip := 1
	if pl.faceLeft {
		flip = -1
	}
	offsetX := 19
	if pl.state != psIdle && pl.state != psRunning {
		offsetX = 15
	}
	offsetY := 11
	switch pl.state {
	case psJumping:
		offsetY = 8
	case psClimbing:
		offsetY = 9
	}
	for i := range pl.shots {
		if !pl.shots[i].active {
			pl.shots[i] = shot{
				active: true,
				x:      pl.box.x + pl.box.w/2 + flip*offsetX,
				y:      pl.box.y + offsetY,
				vx:     flip * 4,
			}
			break
		}
	}
}

func (pl *player) doMovement(room *room, attribs []tileAttrib, input, prev playerInput) {
	pl.vx = 0
	onGround := room.clipY(attribs, pl.box, 1) == 0
	if !onGround {
		pl.vy += 0x40
		if pl.vy > playerVMax {
			pl.vy = playerVMax
		}
	} else {
		pl.vy = 0
	}

	if input.left {
		pl.vx -= 0x200
	}
	if input.right {
		pl.vx += 0x200
	}
	if input.jump && !prev.jump && onGround {
		pl.vy = playerJumpSpeed
	}
	if !input.jump && pl.vy < -0x021f {
		pl.vy = 0
	}
	if input.down && onGround {
		senseX := pl.box.x + pl.box.w/2
		senseY := pl.box.y + pl.box.h
		if room.getTileAttribAtPixel(attribs, senseX, senseY) == attribLadder {
			pl.box.x = (senseX / 16) * 16
			pl.box.y = senseY - 8
			pl.vx, pl.vy = 0, 0
			pl.state = psClimbing
			return
		} else if input.jump && !prev.jump {
			pl.state = psSliding
			if pl.box.h == playerHeight {
				pl.box.y += 8
				pl.box.h -= 8
			}
			pl.slideFrames = 24
			pl.shootFrames = 0
			pl.vy = 0
			return
		}
	}
	if input.up {
		senseX := pl.box.x + pl.box.w/2
		senseY := pl.box.y + pl.box.h/2
		if room.getTileAttribAtPixel(attribs, senseX, senseY) == attribLadder {
			pl.box.x = (senseX / 16) * 16
			pl.vx = 0
			pl.state = psClimbing
			return
		}
	}

	if pl.vx == 0 {
		pl.state = psIdle
	} else if pl.vx > 0 {
		pl.state = psRunning
		pl.faceLeft = false
	} else {
		pl.state = psRunning
		pl.faceLeft = true
	}
	if !onGround {
		pl.state = psJumping
	}
}

func (pl *player) doClimbing(room *room, attribs []tileAttrib, input, prev playerInput) {
	if input.left {
		pl.faceLeft = true
	}
	if input.right {
		pl.faceLeft = false
	}
	pl.vy = 0
	if input.up {
		pl.vy = -0x0100
	}
	if input.down {
		if room.clipY(attribs, pl.box, 1) == 0 {
			pl.state = psIdle
		} else {
			pl.vy = 0x0100
		}
	}
	if input.jump && !prev.jump && !input.up {
		pl.state = psJumping
		pl.vy = 0
		return
	}

	senseX := pl.box.x + pl.box.w/2
	senseY := pl.box.y + pl.box.h/2
	if room.getTileAttribAtPixel(attribs, senseX, senseY) == attribNone {
		tileY := floorDiv(senseY, 16)
		if room.getTileAttribAtPixel(attribs, senseX, (tileY+1)*16) == attribLadder {
			pl.box.y = tileY*16 - 8
		}
		pl.vy = 0
		pl.state = psIdle
		return
	}
}

func (pl *player) doSliding(room *room, attribs []tileAttrib, input, prev playerInput) {
	standUp := pl.slideFrames == 0

	if input.left && !prev.left && !pl.faceLeft {
		pl.faceLeft = true
		standUp = true
	}
	if input.right && !prev.right && pl.faceLeft {
		pl.faceLeft = false
		standUp = true
	}
	if pl.faceLeft {
		pl.vx = -0x300
	} else {
		pl.vx = 0x300
	}

	onGround := room.clipY(attribs, pl.box, 1) == 0
	if input.jump && !prev.jump && onGround {
		pl.vy = playerJumpSpeed
		standUp = true
	}

	if standUp || !onGround {
		pl.slideFrames = 0
		if !onGround {
			pl.state = psJumping
		} else {
			pl.state = psIdle
		}
		pl.box.y -= 8
		pl.box.h += 8
		if room.overlap(attribs, pl.box) {
			pl.box.y += 8
			pl.box.h -= 8
			pl.vy = 0
			pl.state = psSliding
		} else {
			return
		}
	}
}

func (pl *player) doHurting(room *room, attribs []tileAttrib) {
	onGround := room.clipY(attribs, pl.box, 1) == 0
	if !onGround {
		pl.vy += 0x40
		if pl.vy > playerVMax {
			pl.vy = playerVMax
		}
	} else {
		pl.vy = 0
	}
	if pl.faceLeft {
		pl.vx = 0x100
	} else {
		pl.vx = -0x100
	}
	if pl.invincibility < 30 {
		pl.state = psIdle
	}
}
