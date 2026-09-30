package cartridge

// Cartridge is one app. The platform calls Start exactly once when the cart
// is launched (a fresh Start on every launch) and Update once per frame until
// the exit chord is held.
//
// A cart owns the frame for the duration of Update: it receives the shared
// backbuffer via p.Frame() and must leave a full frame behind. Carts keep all
// of their state on their own struct and rebuild it in Start; there is no
// package-level mutable cart state.
type Cartridge interface {
	Name() string
	Start(p *Platform)
	Update(p *Platform)
}

// Factory constructs a cart. The runtime stores factories, not instances, so
// every launch is a fresh Start on a zero-valued cart.
type Factory struct {
	Name string
	New  func() Cartridge
}

// Platform is the cart-facing half of the runtime: a backbuffer to draw into
// and this frame's controls. It never touches the panel or the pins itself.
type Platform struct {
	frame     []uint16
	buttons   Buttons
	prev      Buttons
	backlight uint8
}

// Frame returns the shared Width*Height RGB565 backbuffer, row-major.
func (p *Platform) Frame() []uint16 { return p.frame }

// Buttons returns this frame's control snapshot.
func (p *Platform) Buttons() Buttons { return p.buttons }

// Backlight requests a panel backlight brightness for this frame (0..255).
// The runtime restores full brightness at the start of every frame, so a cart
// must set it each frame it wants it dimmed or pulsing.
func (p *Platform) Backlight(level uint8) { p.backlight = level }

func pressed(now, was bool) bool { return now && !was }

// --- drawing helpers (shared by carts and the menu) ---

// clear fills the whole frame with one color.
func (p *Platform) clear(c uint16) {
	for i := range p.frame {
		p.frame[i] = c
	}
}

// pixel writes one clipped pixel.
func (p *Platform) pixel(x, y int, c uint16) {
	if x < 0 || y < 0 || x >= Width || y >= Height {
		return
	}
	p.frame[y*Width+x] = c
}

// fillRect fills a clipped rectangle.
func (p *Platform) fillRect(x, y, w, h int, c uint16) {
	for yy := y; yy < y+h; yy++ {
		if yy < 0 || yy >= Height {
			continue
		}
		row := yy * Width
		for xx := x; xx < x+w; xx++ {
			if xx < 0 || xx >= Width {
				continue
			}
			p.frame[row+xx] = c
		}
	}
}

// drawText renders s with the 8x8 bitmap font, one 8x8 cell per byte. Every
// cell is painted, foreground or background, so callers get an opaque label.
func (p *Platform) drawText(x, y int, s string, fg, bg uint16) {
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch < 0x20 || ch > 0x7E {
			ch = ' '
		}
		glyph := font8x8[ch-0x20]
		for row := 0; row < 8; row++ {
			bits := glyph[row]
			for col := 0; col < 8; col++ {
				c := bg
				if bits&(0x80>>uint(col)) == 0 {
					c = fg
				}
				p.pixel(x+i*8+col, y+row, c)
			}
		}
	}
}

// --- runtime ---

type runState int

const (
	stMenu runState = iota
	stStart
	stRun
)

// Runner owns the shared backbuffer, the menu, and the cart lifecycle. Call
// Step once per frame; Step paces itself through Env.
type Runner struct {
	p    *Platform
	env  Env
	disp Display
	lib  []Factory

	frameMs uint32
	now     uint32

	state  runState
	sel    int
	active Cartridge
	crash  string

	chordSince uint32
	chordArmed bool

	log func(string)
}

// NewRunner wires a runtime to a display and an environment.
func NewRunner(env Env, disp Display, lib []Factory) *Runner {
	return &Runner{
		p:          &Platform{frame: make([]uint16, Width*Height), backlight: 255},
		env:        env,
		disp:       disp,
		lib:        lib,
		frameMs:    1000 / 60,
		state:      stMenu,
		chordArmed: true,
	}
}

// SetFrameMillis changes the target frame pace (default ~60 fps).
func (r *Runner) SetFrameMillis(ms uint32) { r.frameMs = ms }

// SetLogger attaches an optional sink for lifecycle events (launch, exit, or a
// recovered panic). It is how the firmware surfaces state on USB-CDC.
func (r *Runner) SetLogger(f func(string)) { r.log = f }

func (r *Runner) logf(s string) {
	if r.log != nil {
		r.log(s)
	}
}

// Run steps forever. The badge firmware never returns from here.
func (r *Runner) Run() {
	for {
		r.Step()
	}
}

// Step advances exactly one frame: sample input, run the current state, draw,
// present, and pace. Pacing sleeps through Env.Sleep, so a host environment
// can make it a no-op.
func (r *Runner) Step() {
	r.p.prev = r.p.buttons
	r.p.buttons = r.env.Buttons()
	r.now = r.env.Millis()
	frameStart := r.now

	// Full backlight every frame; a cart may lower it for this frame only.
	r.p.backlight = 255

	switch r.state {
	case stMenu:
		r.stepMenu()
	case stStart:
		r.stepStart()
	case stRun:
		r.stepRun()
	}

	r.disp.Present(r.p.frame)
	r.disp.SetBacklight(r.p.backlight)

	if elapsed := r.env.Millis() - frameStart; elapsed < r.frameMs {
		r.env.Sleep(r.frameMs - elapsed)
	}
}

func (r *Runner) stepMenu() {
	p := r.p
	if pressed(p.buttons.Up, p.prev.Up) && r.sel > 0 {
		r.sel--
	}
	if pressed(p.buttons.Down, p.prev.Down) && r.sel < len(r.lib)-1 {
		r.sel++
	}
	r.drawMenu()
	if pressed(p.buttons.A, p.prev.A) && len(r.lib) > 0 {
		r.active = r.lib[r.sel].New()
		r.crash = ""
		r.state = stStart
		r.logf("launch: " + r.active.Name())
	}
}

func (r *Runner) stepStart() {
	p := r.p
	p.clear(0)
	if r.safeStart(r.active) {
		r.logf("recovered start panic: " + r.crash)
		r.active = nil
		r.state = stMenu
		r.drawMenu()
		return
	}
	r.state = stRun
	// Arm the exit chord only once it is released, so holding Start+Select
	// across a launch does not immediately bounce back to the menu.
	r.chordSince = 0
	r.chordArmed = !(p.buttons.Start && p.buttons.Select)
	r.stepRun()
}

func (r *Runner) stepRun() {
	p := r.p
	if p.buttons.Start && p.buttons.Select {
		if r.chordArmed {
			if r.chordSince == 0 {
				r.chordSince = r.now
			}
			if r.now-r.chordSince >= 250 {
				r.logf("exit: start+select")
				r.active = nil
				r.state = stMenu
				r.crash = ""
				r.drawMenu()
				return
			}
		}
	} else {
		r.chordArmed = true
		r.chordSince = 0
	}

	if r.safeUpdate(r.active) {
		r.logf("recovered update panic: " + r.crash)
		r.active = nil
		r.state = stMenu
		r.drawMenu()
	}
}

// safeStart and safeUpdate run cart code inside recover. A Go panic returns
// the runner to the menu; a hard fault or infinite loop still takes the chip
// down, which is accepted.
func (r *Runner) safeStart(c Cartridge) (crashed bool) {
	defer func() {
		if rec := recover(); rec != nil {
			r.crash = panicText(rec)
			crashed = true
		}
	}()
	c.Start(r.p)
	return false
}

func (r *Runner) safeUpdate(c Cartridge) (crashed bool) {
	defer func() {
		if rec := recover(); rec != nil {
			r.crash = panicText(rec)
			crashed = true
		}
	}()
	c.Update(r.p)
	return false
}

func panicText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		return "cartridge panicked"
	}
}

// --- menu ---

var (
	colBg     = RGB565(0x10, 0x18, 0x28)
	colText   = RGB565(0xE0, 0xE8, 0xF0)
	colDim    = RGB565(0x70, 0x80, 0x98)
	colSelBg  = RGB565(0x30, 0x60, 0xC0)
	colAccent = RGB565(0xFF, 0xC0, 0x40)
	colErr    = RGB565(0xFF, 0x50, 0x50)
)

func (r *Runner) drawMenu() {
	p := r.p
	p.clear(colBg)

	title := "SYCL BADGE"
	p.drawText((Width-len(title)*8)/2, 4, title, colAccent, colBg)
	sub := "SELECT A CART"
	p.drawText((Width-len(sub)*8)/2, 18, sub, colDim, colBg)

	y := 42
	for i, f := range r.lib {
		if i == r.sel {
			p.fillRect(4, y-2, Width-8, 12, colSelBg)
			p.drawText(10, y, f.Name, colText, colSelBg)
			p.drawText(Width-18, y, ">", colText, colSelBg)
		} else {
			p.drawText(10, y, f.Name, colText, colBg)
		}
		y += 14
	}

	if r.crash != "" {
		msg := "PANIC:" + r.crash
		if len(msg) > 20 {
			msg = msg[:20]
		}
		p.drawText(8, 88, msg, colErr, colBg)
	}

	p.drawText(8, 104, "A RUN", colDim, colBg)
	p.drawText(8, 116, "START+SELECT EXIT", colDim, colBg)
}
