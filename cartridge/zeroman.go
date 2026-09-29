package cartridge

// A port of showcase/carts/zeroman (the "needleman" stage). All of the
// reference's package-level mutable state lives on the zeroman struct, which
// the runtime rebuilds from scratch in Start. Rendering writes straight into
// the shared backbuffer; the platform owns the panel and SPI.

const (
	textW = Width / 8
	textH = Height / 8
)

const zDoorDuration = 16

type zState uint8

const (
	zTitle zState = iota
	zStart
	zPlaying
	zGameOver
)

type zTransition uint8

const (
	trNone zTransition = iota
	trVertical
	trDoorLTR
	trDoorRTL
)

type rngState uint32

func (r *rngState) next() uint32 {
	x := uint32(*r)
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	*r = rngState(x)
	return x
}

func (r *rngState) rangeLessThan(lo, hi uint32) uint32 {
	return lo + r.next()%(hi-lo)
}

func subSat(a, b uint8) uint8 {
	if b > a {
		return 0
	}
	return a - b
}

type zeroman struct {
	p     *Platform
	frame []uint16

	scrollX, scrollY int

	state       zState
	counter     uint8
	titleAnyKey bool

	player    player
	input     playerInput
	prevInput playerInput
	enemies   [16]enemy

	curRoom, prevRoom int
	door1H, door2H    uint8

	scrollr box

	transition zTransition
	modeFrame  int
	transY0    int
	transY1    int

	text       [textW * textH]byte
	deathFrame uint32
	rng        rngState
}

// NewZeroman returns the zeroman cartridge.
func NewZeroman() Cartridge { return &zeroman{} }

// Name implements Cartridge.
func (z *zeroman) Name() string { return "ZEROMAN" }

// Start rebuilds all game state and seeds the RNG.
func (z *zeroman) Start(p *Platform) {
	z.p = p
	z.frame = p.Frame()
	z.rng = 0x2545F491
	z.reset()
}

// Update ticks the game and redraws the full frame.
func (z *zeroman) Update(p *Platform) {
	z.p = p
	z.frame = p.Frame()
	z.tick()
	z.draw()
}

func (z *zeroman) scanInput() playerInput {
	b := z.p.Buttons()
	return playerInput{
		left:  b.Left,
		right: b.Right,
		up:    b.Up,
		down:  b.Down,
		jump:  b.A,
		shoot: b.B,
	}
}

func (z *zeroman) reset() {
	z.state = zTitle
	z.counter = 0
	z.titleAnyKey = false
	z.input = playerInput{}
	z.prevInput = playerInput{}
	z.player.reset()
	z.curRoom = 0
	z.prevRoom = 0
	z.transition = trNone
	z.modeFrame = 0
	z.deathFrame = 0
	z.scrollX, z.scrollY = 0, 0

	room := &zeromanStage.rooms[z.curRoom]
	z.scrollr = box{room.bounds.x + 50, room.bounds.y + 100, Width, Height}
	for _, e := range room.entities {
		if e.class == classPlayer {
			z.player.box = e.box
			break
		}
	}
	z.deactivateEnemies()
	z.activateEnemies(room)
}

func (z *zeroman) activateEnemies(room *room) {
	i := 0
	for _, e := range room.entities {
		if e.class == classIguana && i < len(z.enemies) {
			z.enemies[i].activate(enemyIguana, e.box)
			i++
		}
	}
}

func (z *zeroman) deactivateEnemies() {
	for i := range z.enemies {
		z.enemies[i].active = false
	}
}

// --- text layer ---

func (z *zeroman) clearText() {
	for i := range z.text {
		z.text[i] = ' '
	}
}

func (z *zeroman) setText(s string, x, y int) {
	copy(z.text[textW*y+x:], s)
}

// --- rendering ---

func (z *zeroman) clear(c uint16) {
	for i := range z.frame {
		z.frame[i] = c
	}
}

// drawSprite blits a (sx,sy,sw,sh) sub-rectangle of img at (dx,dy), offset by
// the scroll, skipping the transparent sentinel. flipX mirrors within sw.
func (z *zeroman) drawSprite(img *gfxImage, sx, sy, sw, sh int, flipX bool, dx, dy int) {
	for row := 0; row < sh; row++ {
		dstY := dy + row - z.scrollY
		if dstY < 0 || dstY >= Height {
			continue
		}
		line := dstY * Width
		for col := 0; col < sw; col++ {
			dstX := dx + col - z.scrollX
			if dstX < 0 || dstX >= Width {
				continue
			}
			srcX := sx + col
			if flipX {
				srcX = sx + sw - 1 - col
			}
			c := img.at((sy+row)*img.w + srcX)
			if c == transparentKey {
				continue
			}
			z.frame[line+dstX] = c
		}
	}
}

// drawTilemap draws a tile map (data, mapW columns, tiles as a 16-wide sheet)
// clipped to the visible screen area. rect is in world space, tileSize the
// tile edge. Ported from Renderer.Tilemap.draw, with the off-screen loop
// bounds pre-clamped for speed.
func (z *zeroman) drawTilemap(data []byte, mapW int, tiles *gfxImage, r rect, tileSize int) {
	dstX0 := r.x - z.scrollX
	dstY0 := r.y - z.scrollY
	x0 := clampInt(-dstX0, 0, r.w)
	x1 := clampInt(Width-dstX0, 0, r.w)
	y0 := clampInt(-dstY0, 0, r.h)
	y1 := clampInt(Height-dstY0, 0, r.h)
	for y := y0; y < y1; y++ {
		dstY := dstY0 + y
		tileY := y / tileSize
		line := dstY * Width
		rowBase := tileY * mapW
		for x := x0; x < x1; x++ {
			tileIndex := int(data[rowBase+x/tileSize])
			if tileIndex == 0 {
				continue
			}
			srcX := (tileIndex%16)*tileSize + x%tileSize
			srcY := (tileIndex/16)*tileSize + y%tileSize
			c := tiles.at(srcY*tiles.w + srcX)
			if c == transparentKey {
				continue
			}
			z.frame[line+dstX0+x] = c
		}
	}
}

func (z *zeroman) drawText() {
	z.scrollX, z.scrollY = 0, 0
	z.drawTilemap(z.text[:], textW, &gfxFont, rect{0, 0, Width, Height}, 8)
}

// --- tick ---

func (z *zeroman) tick() {
	z.clearText()
	z.prevInput = z.input
	z.input = z.scanInput()
	switch z.state {
	case zTitle:
		z.tickTitle()
	case zStart:
		z.tickStart()
	case zPlaying:
		z.tickPlaying()
	case zGameOver:
		z.tickGameOver()
	}
}

func (z *zeroman) tickTitle() {
	if z.counter%8 < 4 {
		z.setText("PRESS ANY KEY", textW/2-6, textH/2+3)
	}
	if z.input.left || z.input.right || z.input.up || z.input.down || z.input.jump {
		z.titleAnyKey = true
	}
	if z.titleAnyKey {
		z.counter++
	}
	if z.counter == 80 {
		z.counter = 0
		z.state = zStart
	}
}

func (z *zeroman) tickStart() {
	if z.counter%16 >= 8 && z.counter < 32 {
		z.setText("READY", textW/2-2, textH/2)
	}
	z.counter++
	if z.counter >= 48 {
		z.counter = 0
		z.state = zPlaying
	}
}

func (z *zeroman) tickGameOver() {
	if z.counter > 60 {
		z.setText("GAME OVER", textW/2-4, textH/2)
		if z.input.jump && !z.prevInput.jump {
			z.reset()
		}
	} else {
		z.counter++
	}
	for i := range z.enemies {
		if z.enemies[i].active {
			z.enemies[i].tick(z, zeromanStage.attribs)
		}
	}
}

func (z *zeroman) tickPlaying() {
	if z.transition != trNone {
		switch z.transition {
		case trVertical:
			z.doVerticalRoomTransition()
		case trDoorLTR:
			z.doLtrDoorTransition()
		case trDoorRTL:
			z.doRtlDoorTransition()
		}
		return
	}

	z.updatePlayer()
	for i := range z.enemies {
		if z.enemies[i].active {
			z.enemies[i].tick(z, zeromanStage.attribs)
		}
	}

	// player shots vs enemies
	for i := range z.player.shots {
		s := &z.player.shots[i]
		if !s.active {
			continue
		}
		b := box{s.x - 4, s.y - 3, 8, 6}
		for j := range z.enemies {
			e := &z.enemies[j]
			if e.active && b.overlaps(e.box) {
				e.hurt(1)
				s.active = false
				break
			}
		}
	}

	if next, ok := z.findNextRoom(z.curRoom, z.player.box); ok {
		z.setNextRoom(next)
		z.transition = trVertical
		z.modeFrame = 0
		z.beginVerticalTransition()
	}

	room := &zeromanStage.rooms[z.curRoom]
	if !room.bounds.overlaps(z.player.box) {
		if z.player.box.y > room.bounds.y+room.bounds.h {
			z.player.hurt(100)
		}
	}

	for _, ent := range room.entities {
		if ent.class == classSpike && z.player.box.overlaps(ent.box) {
			z.player.hurt(100)
		}
	}

	if z.player.health == 0 {
		z.state = zGameOver
		z.counter = 0
		z.deathFrame = 0
		return
	}

	if room.door1Y != noDoor {
		db := box{room.bounds.x, room.bounds.y + room.door1Y*16, 16, 4 * 16}
		if z.player.box.overlaps(db) {
			db.x--
			if next, ok := z.findNextRoom(z.curRoom, db); ok {
				z.setNextRoom(next)
				z.transition = trDoorRTL
				z.modeFrame = 0
			}
		}
	}
	if room.door2Y != noDoor {
		db := box{room.bounds.x + room.bounds.w - 16, room.bounds.y + room.door2Y*16, 16, 4 * 16}
		if z.player.box.overlaps(db) {
			db.x++
			if next, ok := z.findNextRoom(z.curRoom, db); ok {
				z.setNextRoom(next)
				z.transition = trDoorLTR
				z.modeFrame = 0
			}
		}
	}
}

func (z *zeroman) updatePlayer() {
	z.player.tick()

	room := &zeromanStage.rooms[z.curRoom]
	oldX := z.player.box.x
	z.player.handleInput(room, zeromanStage.attribs, z.input, z.prevInput)
	z.player.move(room, zeromanStage.attribs)

	if z.player.box.x != oldX {
		diff := z.player.box.x - oldX
		targetX := z.player.box.x + 8 - Width/2
		if z.scrollr.x < targetX && diff > 0 {
			z.scrollr.x += diff
		}
		if z.scrollr.x > targetX && diff < 0 {
			z.scrollr.x += diff
		}
	}
	if z.scrollr.x < room.bounds.x {
		z.scrollr.x = room.bounds.x
	}
	if z.scrollr.x > room.bounds.x+room.bounds.w-Width {
		z.scrollr.x = room.bounds.x + room.bounds.w - Width
	}
	z.followPlayerY(room)
}

// followPlayerY keeps the player vertically centred, clamped to the room. The
// reference cart never scrolls vertically, but its rooms are 240px tall on a
// 128px screen, so without this the player leaves the view on ladders and falls.
func (z *zeroman) followPlayerY(room *room) {
	targetY := z.player.box.y + z.player.box.h/2 - Height/2
	if targetY < room.bounds.y {
		targetY = room.bounds.y
	}
	if maxY := room.bounds.y + room.bounds.h - Height; targetY > maxY {
		targetY = maxY
	}
	z.scrollr.y = targetY
}

// beginVerticalTransition records the scroll's start and end Y for a room-to-room
// vertical move, so the camera slides smoothly instead of jumping to the new room.
func (z *zeroman) beginVerticalTransition() {
	cur := &zeromanStage.rooms[z.curRoom]
	prev := &zeromanStage.rooms[z.prevRoom]
	z.transY0 = z.scrollr.y
	finalY := prev.bounds.y - z.player.box.h
	if cur.bounds.y >= prev.bounds.y+prev.bounds.h {
		finalY = cur.bounds.y
	}
	targetY := finalY + z.player.box.h/2 - Height/2
	if targetY < cur.bounds.y {
		targetY = cur.bounds.y
	}
	if maxY := cur.bounds.y + cur.bounds.h - Height; targetY > maxY {
		targetY = maxY
	}
	z.transY1 = targetY
}

func (z *zeroman) findNextRoom(skip int, b box) (int, bool) {
	for i := range zeromanStage.rooms {
		if i == skip {
			continue
		}
		if zeromanStage.rooms[i].bounds.overlaps(b) {
			return i, true
		}
	}
	return 0, false
}

func (z *zeroman) setNextRoom(next int) {
	z.prevRoom = z.curRoom
	z.curRoom = next
}

func (z *zeroman) doVerticalRoomTransition() {
	z.modeFrame++
	cur := &zeromanStage.rooms[z.curRoom]
	prev := &zeromanStage.rooms[z.prevRoom]
	if cur.bounds.y >= prev.bounds.y+prev.bounds.h {
		z.player.box.y = cur.bounds.y - z.player.box.h + (z.modeFrame*z.player.box.h)/60
	}
	if cur.bounds.y+cur.bounds.h <= prev.bounds.y {
		z.player.box.y = prev.bounds.y - (z.modeFrame*z.player.box.h)/60
	}
	z.scrollr.y = z.transY0 + ((z.transY1-z.transY0)*z.modeFrame)/60
	if z.modeFrame == 60 {
		z.transition = trNone
	}
}

func (z *zeroman) doLtrDoorTransition() {
	z.modeFrame++
	if z.modeFrame <= zDoorDuration {
		z.door1H = uint8(4 - (4*z.modeFrame)/zDoorDuration)
	} else if z.modeFrame <= zDoorDuration+64 {
		z.player.tick()
		cur := &zeromanStage.rooms[z.curRoom]
		z.scrollr.x = cur.bounds.x - Width + ((z.modeFrame-zDoorDuration)*Width)/64
		z.player.box.x = cur.bounds.x - 2*z.player.box.w + (3*z.player.box.w*(z.modeFrame-zDoorDuration))/64
	} else if z.modeFrame <= zDoorDuration+64+zDoorDuration {
		z.door1H = uint8((4 * (z.modeFrame - 64 - zDoorDuration)) / zDoorDuration)
	}
	if z.modeFrame == zDoorDuration+64+zDoorDuration {
		z.transition = trNone
	}
}

func (z *zeroman) doRtlDoorTransition() {
	z.modeFrame++
	if z.modeFrame <= zDoorDuration {
		z.door2H = uint8(4 - (4*z.modeFrame)/zDoorDuration)
	} else if z.modeFrame <= zDoorDuration+64 {
		z.player.tick()
		prev := &zeromanStage.rooms[z.prevRoom]
		z.scrollr.x = prev.bounds.x - ((z.modeFrame-zDoorDuration)*Width)/64
		z.player.box.x = prev.bounds.x + z.player.box.w - (3*z.player.box.w*(z.modeFrame-zDoorDuration))/64
	} else if z.modeFrame <= zDoorDuration+64+zDoorDuration {
		z.door2H = uint8((4 * (z.modeFrame - 64 - zDoorDuration)) / zDoorDuration)
	}
	if z.modeFrame == zDoorDuration+64+zDoorDuration {
		z.transition = trNone
	}
}

// --- draw ---

func (z *zeroman) draw() {
	z.clear(0)
	z.scrollX, z.scrollY = 0, 0

	if z.state == zTitle {
		z.drawTitle()
	} else {
		z.scrollX = z.scrollr.x
		z.scrollY = z.scrollr.y

		if z.transition != trNone {
			z.drawRoom(z.prevRoom, z.door2H, z.door1H)
		}
		z.drawRoom(z.curRoom, z.door1H, z.door2H)

		for i := range z.enemies {
			if z.enemies[i].active {
				z.enemies[i].draw(z)
			}
		}

		if z.state == zStart {
			px := z.player.box.x + z.player.box.w/2
			py := z.player.box.y + z.player.box.h
			drawTeleportEffect(z, px, py, z.counter)
		} else if z.state == zGameOver {
			if z.deathFrame < 40 && z.deathFrame%8 < 4 {
				z.player.draw(z)
			}
			drawDeathEffect(z, z.player.box.x-4, z.player.box.y, z.deathFrame)
			z.deathFrame++
		} else {
			z.player.draw(z)
		}
	}

	z.scrollX, z.scrollY = 0, 0
	if z.state != zTitle {
		z.drawHealthbar()
	}
	z.drawText()
}

func (z *zeroman) drawTitle() {
	z.drawSprite(&gfxTitle, 0, 0, 160, 48, false, 0, 32)
}

func (z *zeroman) drawHealthbar() {
	z.drawSprite(&gfxHealthbar, 0, 0, 12, 68, false, 22, 14)
	h := 4 + (31-int(z.player.health))*2
	z.drawSprite(&gfxHealthbar, 12, 0, 12, h, false, 22, 14)
}

func (z *zeroman) drawRoom(idx int, door1H, door2H uint8) {
	r := &zeromanStage.rooms[idx]
	z.drawTilemap(r.data, r.width, &gfxNeedleman, r.bounds.toRect(), 16)

	if r.door1Y != noDoor {
		for i := 0; i < int(door1H); i++ {
			z.drawSprite(&gfxDoor, 0, 0, 16, 16, false, r.bounds.x, r.bounds.y+(r.door1Y+i)*16)
		}
	}
	if r.door2Y != noDoor {
		for i := 0; i < int(door2H); i++ {
			z.drawSprite(&gfxDoor, 0, 0, 16, 16, false, r.bounds.x+r.bounds.w-16, r.bounds.y+(r.door2Y+i)*16)
		}
	}
	for _, e := range r.entities {
		if e.class == classSpike {
			z.drawSprite(&gfxSpike, 0, 0, gfxSpike.w, gfxSpike.h, false, e.box.x, e.box.y)
		}
	}
}
