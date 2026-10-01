package main

import (
	"machine"
	"time"
)

// LCD wiring on the SYCL Badge V2. Mirrors the reference firmware
// (src/board_v2.zig and src/os/drivers/lcd.zig).
//
// The panel is a DT018BTFT-SHB: 1.8", 160x128, ST7735S-class controller on
// SPI0. It is a bare panel with no usable reset GPIO (RST is tied to the
// RP2354B reset) and no MISO.
const (
	panelWidth  = 160
	panelHeight = 128

	lcdCS   = machine.GPIO17
	lcdSCK  = machine.GPIO18
	lcdMOSI = machine.GPIO19
	lcdDC   = machine.GPIO21
	lcdBL   = machine.GPIO16

	// The panel and wiring tolerate the reference firmware's 62.5 MHz, which
	// keeps a full 160x128 frame (40 KB) under the 60 fps budget.
	lcdSPIFreq = 62_500_000
)

// ST7735S command set used by the init sequence.
const (
	cmdSWRESET = 0x01
	cmdSLPOUT  = 0x11
	cmdNORON   = 0x13
	cmdINVOFF  = 0x20
	cmdDISPON  = 0x29
	cmdCASET   = 0x2A
	cmdRASET   = 0x2B
	cmdRAMWR   = 0x2C
	cmdTEARON  = 0x35
	cmdMADCTL  = 0x36
	cmdCOLMOD  = 0x3A
	cmdGAMSET  = 0x26
	cmdGAMADJ  = 0xF2
	cmdFRMCTR1 = 0xB1
	cmdINVCTR  = 0xB4
	cmdPWCTR1  = 0xC0
	cmdPWCTR2  = 0xC1
	cmdVMCTR1  = 0xC5
	cmdVMOFF   = 0xC7
	cmdGMCTRP1 = 0xE0
	cmdGMCTRN1 = 0xE1
)

// madctlLandscape = MX | MV. This is the value the reference firmware uses to
// rotate the native 128x160 panel into a 160x128 landscape canvas: with MV (=row
// /column exchange) the column address space becomes 160 wide, which is exactly
// what the draw calls below assume.
//
// The previous code used the tinygo st7735 driver at rotation 0 (MADCTL=0xC0,
// no MV) while addressing 160 columns. On this panel the column (source) axis is
// only 128 deep without MV, so every 160-wide write overran the RAM, wrapped,
// and produced the diagonal/streaky garbage seen on hardware.
const madctlLandscape = 0x60

var (
	csPin = lcdCS
	dcPin = lcdDC
)

// Backlight PWM. GPIO16 gates the badge's TPS61041 backlight boost converter
// (BKLT_EN): the PWM drives the boost's enable input, and its FB node is tied
// to the LED sense rail, so the duty cycle literally modulates the LED current
// rail (a 1 kHz enable "chop"), not a brightness-encoded PWM carrier. The
// reference firmware's own backlight PWM is clk_div=150, wrap=1023 -- about
// 1 kHz. A ~200 kHz carrier on this enable input does not produce any average
// brightness: below near-full duty the boost settles dark, which is why the
// card lightshow's backlight breathing read as a long blackout. Match the
// reference's 1 kHz; even so, duty resolution outclasses the 0..255 levels we
// drive. The boost's LED rail is wired through the panel connector, so this is
// an enable/duty control for the whole show.
var (
	frontlightPWM = machine.PWM0
	frontlightCh  uint8
)

// frontlightPWMPeriod is the PWM period in nanoseconds (~1 kHz, as the
// reference firmware drives it).
const frontlightPWMPeriod = 1_000_000

// lcdInit configures the pins and SPI bus, then runs the panel init sequence
// copied from the reference firmware's init_display().
func lcdInit() {
	csPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
	dcPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
	csPin.High()
	dcPin.High()

	// Backlight on PWM (GPIO16 = the TPS61041's BKLT_EN). Configure it before
	// the panel init and leave the duty at zero (dark) until the panel is on,
	// then raise it to full.
	if err := frontlightPWM.Configure(machine.PWMConfig{Period: frontlightPWMPeriod}); err != nil {
		panic("lcd: backlight pwm: " + err.Error())
	}
	ch, err := frontlightPWM.Channel(lcdBL)
	if err != nil {
		panic("lcd: backlight channel: " + err.Error())
	}
	frontlightCh = ch

	// SPI0 on GPIO18 (SCK) / GPIO19 (MOSI). SDI is NoPin: the bus has no MISO
	// and, more importantly, the zero value (GPIO0) would otherwise be claimed
	// by the SPI peripheral. GPIO16 is the backlight, not SPI0 RX.
	machine.SPI0.Configure(machine.SPIConfig{
		Frequency: lcdSPIFreq,
		SCK:       lcdSCK,
		SDO:       lcdMOSI,
		SDI:       machine.NoPin,
	})

	// The panel's RESET line is tied to the RP2354B reset, so there is no reset
	// GPIO to pulse. Just let the panel finish its power-on reset.
	time.Sleep(50 * time.Millisecond)

	lcdCmd(cmdSWRESET)
	time.Sleep(120 * time.Millisecond)

	lcdCmd(cmdSLPOUT)
	time.Sleep(5 * time.Millisecond)

	lcdCmdData(cmdCOLMOD, 0x05) // 16-bit/pixel (RGB565)
	lcdCmdData(cmdGAMSET, 0x04)
	lcdCmdData(cmdGAMADJ, 0x01)
	lcdCmdData(cmdGMCTRP1,
		0x3F, 0x25, 0x1C, 0x1E, 0x20, 0x12, 0x2A, 0x90,
		0x24, 0x11, 0x00, 0x00, 0x00, 0x00, 0x00)
	lcdCmdData(cmdGMCTRN1,
		0x20, 0x20, 0x20, 0x20, 0x05, 0x00, 0x15, 0xA7,
		0x3D, 0x18, 0x25, 0x2A, 0x2B, 0x2B, 0x3A)

	lcdCmdData(cmdFRMCTR1, 0x08, 0x08)
	lcdCmdData(cmdINVCTR, 0x07)
	lcdCmdData(cmdPWCTR1, 0x0A, 0x02)
	lcdCmdData(cmdPWCTR2, 0x02)
	lcdCmdData(cmdVMCTR1, 0x50, 0x5B)
	lcdCmdData(cmdVMOFF, 0x40)
	lcdCmdData(cmdTEARON, 0x00)

	// Initial full-panel window (overridden by lcdSetWindow for every draw).
	lcdCmdData(cmdCASET, 0x00, 0x00, 0x00, 0x7F)
	lcdCmdData(cmdRASET, 0x00, 0x00, 0x00, 0x9F)

	time.Sleep(250 * time.Millisecond)

	lcdCmdData(cmdMADCTL, madctlLandscape)

	lcdCmd(cmdINVOFF)
	lcdCmd(cmdNORON)
	lcdCmd(cmdDISPON)

	// Panel is on: bring the backlight to full.
	frontlightPWM.Set(frontlightCh, frontlightPWM.Top())
}

// lcdSetBacklight sets the backlight duty cycle, 0 (off) .. 255 (full).
func lcdSetBacklight(level uint8) {
	frontlightPWM.Set(frontlightCh, uint32(level)*frontlightPWM.Top()/255)
}

// lcdCmd sends a command byte (DC low).
func lcdCmd(cmd byte) {
	dcPin.Low()
	csPin.Low()
	machine.SPI0.Tx([]byte{cmd}, nil)
	csPin.High()
}

// lcdData sends data bytes (DC high).
func lcdData(data ...byte) {
	dcPin.High()
	csPin.Low()
	machine.SPI0.Tx(data, nil)
	csPin.High()
}

// lcdCmdData sends a command followed by its data bytes.
func lcdCmdData(cmd byte, data ...byte) {
	lcdCmd(cmd)
	if len(data) > 0 {
		lcdData(data...)
	}
}

// lcdSetWindow sets the address window and starts a RAM write. Coordinates are
// inclusive, in landscape panel space (0..159, 0..127).
func lcdSetWindow(x0, y0, x1, y1 int16) {
	lcdCmdData(cmdCASET, byte(x0>>8), byte(x0), byte(x1>>8), byte(x1))
	lcdCmdData(cmdRASET, byte(y0>>8), byte(y0), byte(y1>>8), byte(y1))
	lcdCmd(cmdRAMWR)
}

// lcdFillScreen fills the whole panel with an RGB565 value.
func lcdFillScreen(c uint16) {
	lcdSetWindow(0, 0, panelWidth-1, panelHeight-1)

	hi := byte(c >> 8)
	lo := byte(c)
	row := make([]byte, panelWidth*2)
	for i := 0; i < panelWidth; i++ {
		row[2*i] = hi
		row[2*i+1] = lo
	}

	dcPin.High()
	csPin.Low()
	for y := 0; y < panelHeight; y++ {
		machine.SPI0.Tx(row, nil)
	}
	csPin.High()
}

// lcdDrawBitmap writes a w*h RGB565 (high byte first) bitmap at (x, y).
func lcdDrawBitmap(x, y int16, data []byte, w, h int16) {
	lcdSetWindow(x, y, x+w-1, y+h-1)
	dcPin.High()
	csPin.Low()
	machine.SPI0.Tx(data, nil)
	csPin.High()
}

// lcdRowBuf is the reusable row staging buffer for lcdPresent, so presenting a
// frame allocates nothing.
var lcdRowBuf [panelWidth * 2]byte

// lcdPresent flushes a full 160x128 standard-RGB565 frame (row-major, index
// y*panelWidth+x) to the panel in one windowed write, high byte first. It is
// the single SPI touch point for the cartridge runtime.
func lcdPresent(frame []uint16) {
	lcdSetWindow(0, 0, panelWidth-1, panelHeight-1)

	dcPin.High()
	csPin.Low()
	for y := 0; y < panelHeight; y++ {
		off := y * panelWidth
		for x := 0; x < panelWidth; x++ {
			v := frame[off+x]
			lcdRowBuf[2*x] = byte(v >> 8)
			lcdRowBuf[2*x+1] = byte(v)
		}
		machine.SPI0.Tx(lcdRowBuf[:], nil)
	}
	csPin.High()
}

// showGopher paints the panel black and draws the gopher centered.
func showGopher() {
	lcdFillScreen(0x0000)
	lcdDrawBitmap(
		(panelWidth-gopherWidth)/2,
		(panelHeight-gopherHeight)/2,
		gopherData[:],
		gopherWidth,
		gopherHeight,
	)
}
