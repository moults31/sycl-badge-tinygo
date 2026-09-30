// Command sim runs the cartridge runtime on the host, driving a scripted
// button sequence and writing PNG frames so the menu, plasma, the exit chord,
// and panic recovery can be inspected without hardware.
//
//	go run ./cmd/sim [-out sim-out]
package main

import (
	"crypto/md5"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"sycl-badge-tinygo/cartridge"
)

const scale = 3

// stepEnv hands the runtime whatever button state the script last set, and
// advances a fake millisecond clock on Sleep (which is a no-op).
type stepEnv struct {
	cur    cartridge.Buttons
	t      uint32
	stepMs uint32
}

func (e *stepEnv) Buttons() cartridge.Buttons { return e.cur }
func (e *stepEnv) Millis() uint32             { return e.t }
func (e *stepEnv) Sleep(ms uint32)            { e.t += ms }

// captureDisplay keeps the most recent presented frame and backlight level.
type captureDisplay struct {
	last      []uint16
	backlight uint8
}

func (d *captureDisplay) Present(f []uint16) {
	d.last = append(d.last[:0], f...)
}

func (d *captureDisplay) SetBacklight(level uint8) { d.backlight = level }

func main() {
	out := flag.String("out", "sim-out", "directory for rendered PNG frames")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}

	lib := []cartridge.Factory{
		{Name: "PLASMA", New: cartridge.NewPlasma},
		{Name: "ZEROMAN", New: cartridge.NewZeroman},
		{Name: "PANIC TEST", New: cartridge.NewPanicTest},
		{Name: "CARD SHOW", New: cartridge.NewCardShow},
	}

	env := &stepEnv{stepMs: 16}
	disp := &captureDisplay{}
	r := cartridge.NewRunner(env, disp, lib)
	r.SetFrameMillis(16)

	step := func(b cartridge.Buttons, n int) {
		env.cur = b
		for i := 0; i < n; i++ {
			r.Step()
		}
	}
	dump := func(name string) {
		path := filepath.Join(*out, name)
		if err := writePNG(path, disp.last); err != nil {
			fatal(err)
		}
		sum := md5.Sum(frameBytes(disp.last))
		fmt.Printf("%-32s %08x bl=%3d\n", name, sum[:4], disp.backlight)
	}

	var (
		none  = cartridge.Buttons{}
		a     = cartridge.Buttons{A: true}
		down  = cartridge.Buttons{Down: true}
		chord = cartridge.Buttons{Start: true, Select: true}
		sel   = cartridge.Buttons{Select: true}
		right = cartridge.Buttons{Right: true}
	)

	step(none, 3)
	dump("01-menu.png")

	// Launch plasma, then let it animate.
	step(a, 1)
	step(none, 1)
	dump("02-plasma-first.png")
	step(none, 20)
	dump("03-plasma-later.png")

	// Hold the exit chord for well over 250 ms.
	step(chord, 20)
	dump("04-menu-after-exit.png")

	// Relaunch: a fresh Start must reproduce the first frame exactly.
	step(a, 1)
	step(none, 1)
	dump("05-plasma-relaunch-first.png")

	step(chord, 20)
	step(none, 2)

	// Select and launch zeroman: title screen, then let the game start.
	step(down, 1)
	step(a, 1)
	step(none, 1)
	dump("06-zeroman-title.png")
	step(a, 1)     // any key leaves the title
	step(none, 90) // title counter -> start
	step(none, 60) // start -> playing
	dump("07-zeroman-playing.png")
	step(none, 30)
	dump("08-zeroman-playing-later.png")
	step(chord, 20)
	step(none, 2)

	// Select and launch the panic cart; the runtime must recover to the menu.
	step(down, 1)
	dump("09-menu-panic-selected.png")
	step(a, 1)
	step(none, 2)
	dump("10-menu-after-panic.png")

	// Select and launch the card lightshow, then capture every baked card
	// (Left/Right cycle the library), a later breathing frame, an attack
	// flash, and the calibration overlay.
	step(down, 1)
	step(none, 1)
	step(a, 1)
	step(none, 1)
	dump("11-card-00.png")
	for i := 1; i < 5; i++ {
		step(right, 1)
		step(none, 1)
		dump(fmt.Sprintf("12-card-%02d.png", i))
	}
	step(none, 90)
	dump("13-card-breathing.png")
	step(a, 1)
	step(none, 2)
	dump("14-card-attack.png")
	step(none, 30)
	step(sel, 1)
	step(none, 1)
	dump("15-card-calibration.png")

	fmt.Printf("wrote frames to %s/\n", *out)
}

func frameBytes(f []uint16) []byte {
	b := make([]byte, len(f)*2)
	for i, v := range f {
		b[2*i] = byte(v >> 8)
		b[2*i+1] = byte(v)
	}
	return b
}

func rgb565toRGBA(v uint16) color.RGBA {
	r := uint8(v>>11) & 0x1F
	g := uint8(v>>5) & 0x3F
	b := uint8(v) & 0x1F
	return color.RGBA{
		R: r<<3 | r>>2,
		G: g<<2 | g>>4,
		B: b<<3 | b>>2,
		A: 0xFF,
	}
}

func writePNG(path string, frame []uint16) error {
	if len(frame) != cartridge.Width*cartridge.Height {
		return fmt.Errorf("frame has %d pixels, want %d", len(frame), cartridge.Width*cartridge.Height)
	}
	img := image.NewRGBA(image.Rect(0, 0, cartridge.Width*scale, cartridge.Height*scale))
	for y := 0; y < cartridge.Height; y++ {
		for x := 0; x < cartridge.Width; x++ {
			c := rgb565toRGBA(frame[y*cartridge.Width+x])
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetRGBA(x*scale+dx, y*scale+dy, c)
				}
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "sim:", err)
	os.Exit(1)
}
