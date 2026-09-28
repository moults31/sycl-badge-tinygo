//go:build cartdemo

package main

import (
	"time"

	"sycl-badge-tinygo/cartridge"
)

// demoEnv is a build-time hardware self-test (build with -tags=cartdemo). It
// replays the simulator's button script on the real panel and real clock, so
// the launch, exit-chord, fresh-relaunch, and panic-recovery paths can be
// watched and logged over USB-CDC without anyone pressing a button.
type demoStep struct {
	at uint32
	b  cartridge.Buttons
}

type demoEnv struct {
	start time.Time
	seq   []demoStep
}

func selectEnv() cartridge.Env {
	var (
		none  = cartridge.Buttons{}
		a     = cartridge.Buttons{A: true}
		down  = cartridge.Buttons{Down: true}
		chord = cartridge.Buttons{Start: true, Select: true}
	)
	return &demoEnv{
		start: time.Now(),
		seq: []demoStep{
			{0, none},     // menu
			{1000, a},     // launch plasma
			{1100, none},  // plasma runs
			{4000, chord}, // hold the exit chord
			{4700, none},  // back at the menu
			{5000, a},     // fresh relaunch
			{5100, none},  // plasma runs again
			{8000, chord}, // exit again
			{8700, none},
			{9000, down}, // select the panic cart
			{9200, none},
			{9400, a},    // launch it; Update panics
			{9600, none}, // recovered back to the menu
		},
	}
}

func (e *demoEnv) Buttons() cartridge.Buttons {
	t := e.Millis()
	b := cartridge.Buttons{}
	for _, s := range e.seq {
		if t >= s.at {
			b = s.b
		}
	}
	return b
}

func (e *demoEnv) Millis() uint32 {
	return uint32(time.Since(e.start) / time.Millisecond)
}

func (e *demoEnv) Sleep(ms uint32) {
	time.Sleep(time.Duration(ms) * time.Millisecond)
}
