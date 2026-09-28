//go:build !cartdemo

package main

import (
	"time"

	"machine"

	"sycl-badge-tinygo/cartridge"
)

// hwEnv is the badge's cartridge.Env: the button pins, a millisecond clock,
// and a real sleep for frame pacing. Buttons are active high with pull-downs,
// so an unpressed pin reads low.
type hwEnv struct {
	start time.Time
	pins  []machine.Pin
}

func newHWEnv() *hwEnv {
	pins := []machine.Pin{
		machine.BUTTON_START,
		machine.BUTTON_SELECT,
		machine.BUTTON_A,
		machine.BUTTON_B,
		machine.JOYSTICK_CLICK,
		machine.JOYSTICK_UP,
		machine.JOYSTICK_DOWN,
		machine.JOYSTICK_LEFT,
		machine.JOYSTICK_RIGHT,
	}
	for _, p := range pins {
		p.Configure(machine.PinConfig{Mode: machine.PinInputPulldown})
	}
	return &hwEnv{start: time.Now(), pins: pins}
}

func (e *hwEnv) Buttons() cartridge.Buttons {
	at := func(i int) bool { return e.pins[i].Get() }
	return cartridge.Buttons{
		Start:  at(0),
		Select: at(1),
		A:      at(2),
		B:      at(3),
		Click:  at(4),
		Up:     at(5),
		Down:   at(6),
		Left:   at(7),
		Right:  at(8),
	}
}

func (e *hwEnv) Millis() uint32 {
	return uint32(time.Since(e.start) / time.Millisecond)
}

func (e *hwEnv) Sleep(ms uint32) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// selectEnv returns the real button environment for a normal build.
func selectEnv() cartridge.Env { return newHWEnv() }
