package main

// lcdDisplay adapts the ST7735S driver to cartridge.Display.
type lcdDisplay struct{}

func (lcdDisplay) Present(frame []uint16) { lcdPresent(frame) }
