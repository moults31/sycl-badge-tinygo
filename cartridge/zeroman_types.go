package cartridge

// Shared types for the zeroman port: generated art access, geometry, tile
// attributes, rooms and the stage. Coordinates are landscape panel pixels;
// frames are row-major (index y*Width+x).

// transparentKey is the magenta the reference art uses for transparency.
const transparentKey uint16 = 0xF81F

// gfxImage is a palette image with bit-packed indices, as emitted by
// tools/make_zeroman_gfx.py. bits is 1, 2 or 4.
type gfxImage struct {
	w, h    int
	colors  []uint16
	bits    int
	indices []byte
}

// at returns the RGB565 color of pixel i (row-major).
func (g *gfxImage) at(i int) uint16 {
	bit := i * g.bits
	v := int(g.indices[bit>>3]) >> uint(bit&7)
	return g.colors[v&((1<<g.bits)-1)]
}

type tileAttrib uint8

const (
	attribNone tileAttrib = iota
	attribSolid
	attribLadder
)

type box struct{ x, y, w, h int }

func (b box) overlaps(o box) bool {
	return b.x < o.x+o.w && b.x+b.w > o.x && b.y < o.y+o.h && b.y+b.h > o.y
}

func (b box) toRect() rect { return rect{b.x, b.y, b.w, b.h} }

// castX clips a horizontal move of `amount` against other, returning the
// allowed amount. Ported from Box.castX.
func (b box) castX(amount int, other box) int {
	if b.y >= other.y+other.h || b.y+b.h <= other.y {
		return amount
	}
	if amount > 0 && b.x < other.x+other.w {
		if b.x+b.w+amount > other.x {
			return other.x - (b.x + b.w)
		}
	} else if amount < 0 && b.x+b.w > other.x {
		if b.x+amount < other.x+other.w {
			return other.x + other.w - b.x
		}
	}
	return amount
}

// castY clips a vertical move of `amount` against other.
func (b box) castY(amount int, other box) int {
	if b.x >= other.x+other.w || b.x+b.w <= other.x {
		return amount
	}
	if amount > 0 && b.y < other.y+other.h {
		if b.y+b.h+amount > other.y {
			return other.y - (b.y + b.h)
		}
	} else if amount < 0 && b.y+b.h > other.y {
		if b.y+amount < other.y+other.h {
			return other.y + other.h - b.y
		}
	}
	return amount
}

type rect struct{ x, y, w, h int }

type entityClass uint8

const (
	classPlayer entityClass = iota
	classIguana
	classSpike
)

type entity struct {
	class entityClass
	box   box
}

// noDoor marks a room edge with no door.
const noDoor = -1

type room struct {
	bounds   box
	width    int
	height   int
	data     []byte
	entities []entity
	door1Y   int
	door2Y   int
}

type stage struct {
	rooms   []room
	attribs []tileAttrib
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// floorDiv is floor(a/b) for b > 0.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// clipX returns the allowed horizontal move of mover against the room's solid
// tiles. Ported from Room.clipX.
func (r *room) clipX(attribs []tileAttrib, mover box, amount int) int {
	if amount == 0 {
		return 0
	}
	clipped := amount
	if mover.x+clipped < 0 {
		clipped = -mover.x
	}
	b := box{mover.x - r.bounds.x, mover.y - r.bounds.y, mover.w, mover.h}
	var area box
	if clipped > 0 {
		area = box{b.x + b.w, b.y, clipped, b.h}
	} else {
		area = box{b.x + clipped, b.y, -clipped, b.h}
	}
	startX := clampInt(area.x/16, 0, r.width-1)
	stopX := clampInt((area.x+area.w-1)/16, 0, r.width-1)
	startY := clampInt(area.y/16, 0, r.height-1)
	stopY := clampInt((area.y+area.h-1)/16, 0, r.height-1)
	for y := startY; y <= stopY; y++ {
		for x := startX; x <= stopX; x++ {
			if attribs[r.data[y*r.width+x]] == attribSolid {
				clipped = b.castX(clipped, box{x * 16, y * 16, 16, 16})
			}
		}
	}
	return clipped
}

// clipY is the vertical counterpart, including the ladder-top check.
func (r *room) clipY(attribs []tileAttrib, mover box, amount int) int {
	if amount == 0 {
		return 0
	}
	clipped := amount
	b := box{mover.x - r.bounds.x, mover.y - r.bounds.y, mover.w, mover.h}
	var area box
	if clipped > 0 {
		area = box{b.x, b.y + b.h, b.w, clipped}
	} else {
		area = box{b.x, b.y + clipped, b.w, -clipped}
	}
	startX := clampInt(area.x/16, 0, r.width-1)
	stopX := clampInt((area.x+area.w-1)/16, 0, r.width-1)
	startY := clampInt(area.y/16, 0, r.height-1)
	stopY := clampInt((area.y+area.h-1)/16, 0, r.height-1)
	for y := startY; y <= stopY; y++ {
		for x := startX; x <= stopX; x++ {
			attrib := attribs[r.data[y*r.width+x]]
			if attrib == attribSolid {
				clipped = b.castY(clipped, box{x * 16, y * 16, 16, 16})
			} else if attrib == attribLadder && clipped > 0 && y > 0 {
				if r.tileAttribAtTile(attribs, x, y-1) == attribNone {
					clipped = b.castY(clipped, box{x * 16, y * 16, 16, 16})
				}
			}
		}
	}
	return clipped
}

// overlap reports whether mover overlaps any solid tile.
func (r *room) overlap(attribs []tileAttrib, mover box) bool {
	b := box{mover.x - r.bounds.x, mover.y - r.bounds.y, mover.w, mover.h}
	startX := clampInt(floorDiv(b.x, 16), 0, r.width-1)
	stopX := clampInt(floorDiv(b.x+b.w-1, 16), 0, r.width-1)
	startY := clampInt(floorDiv(b.y, 16), 0, r.height-1)
	stopY := clampInt(floorDiv(b.y+b.h-1, 16), 0, r.height-1)
	for y := startY; y <= stopY; y++ {
		for x := startX; x <= stopX; x++ {
			if attribs[r.data[y*r.width+x]] == attribSolid {
				if b.overlaps(box{x * 16, y * 16, 16, 16}) {
					return true
				}
			}
		}
	}
	return false
}

func (r *room) getTileAttribAtPixel(attribs []tileAttrib, x, y int) tileAttrib {
	tx := floorDiv(x-r.bounds.x, 16)
	ty := floorDiv(y-r.bounds.y, 16)
	return r.tileAttribAtTile(attribs, tx, ty)
}

func (r *room) tileAttribAtTile(attribs []tileAttrib, tx, ty int) tileAttrib {
	if tx < 0 || ty < 0 || tx >= r.width || ty >= r.height {
		return attribNone
	}
	return attribs[r.data[ty*r.width+tx]]
}
