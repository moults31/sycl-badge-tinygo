// Package cartridge holds the app-swapping runtime and the cartridges that
// run on it. It is deliberately free of any hardware imports: the platform
// injects a Display and an Env, so the same code runs on the badge and in the
// host simulator (cmd/sim).
package cartridge

// Screen geometry. The panel is natively 128x160; the driver addresses it as
// a 160x128 landscape canvas (MADCTL=0x60, MX|MV).
const (
	Width  = 160
	Height = 128
)

// Buttons is one frame's snapshot of the badge's controls. All fields are the
// level as read this frame (active high, pull-down).
type Buttons struct {
	Start  bool
	Select bool
	A      bool
	B      bool
	Click  bool
	Up     bool
	Down   bool
	Left   bool
	Right  bool
}

// RGB565 packs 8-bit components into a standard RGB565 pixel: R in bits
// 15..11, G in 10..5, B in 4..0. This is the wire format the ST7735S expects
// (high byte first), which is also what tools/make_gopher.py emits.
func RGB565(r, g, b uint8) uint16 {
	return uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
}

// Display presents one finished frame. The frame is Width*Height standard
// RGB565 pixels, row-major (index y*Width+x).
type Display interface {
	Present(frame []uint16)
}

// Env is everything the runtime needs from the world: the current button
// state, a monotonic millisecond clock, and a way to pace frames.
type Env interface {
	Buttons() Buttons
	Millis() uint32
	Sleep(ms uint32)
}
