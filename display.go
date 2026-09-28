package main

import (
	"image/color"
	"machine"
	"time"

	"tinygo.org/x/drivers/st7735"
)

// LCD wiring on the SYCL Badge V2 (see the reference firmware's
// src/board_v2.zig). The panel is a 160x128 ST7735S-class display on SPI0.
const (
	panelWidth  = 160
	panelHeight = 128

	lcdCS   = machine.GPIO17
	lcdSCK  = machine.GPIO18
	lcdMOSI = machine.GPIO19
	lcdDC   = machine.GPIO21
	lcdBL   = machine.GPIO16
)

var (
	black = color.RGBA{0, 0, 0, 255}
)

func newDisplay() *st7735.Device {
	machine.SPI0.Configure(machine.SPIConfig{
		Frequency: 8_000_000,
		SCK:       lcdSCK,
		SDO:       lcdMOSI,
	})

	// The panel's RESET line is tied to the RP2354B reset, so there is no
	// controllable reset GPIO. machine.NoPin is never driven.
	display := st7735.New(machine.SPI0, machine.NoPin, lcdDC, lcdCS, lcdBL)
	display.Configure(st7735.Config{
		Width:  panelWidth,
		Height: panelHeight,
	})

	return &display
}

// showGopher paints the panel black and draws the gopher centered.
func showGopher(display *st7735.Device) {
	display.FillScreen(black)
	time.Sleep(10 * time.Millisecond)
	display.DrawRGBBitmap8(
		(panelWidth-gopherWidth)/2,
		(panelHeight-gopherHeight)/2,
		gopherData[:],
		gopherWidth,
		gopherHeight,
	)
}
