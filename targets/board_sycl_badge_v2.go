//go:build sycl_badge_v2

package machine

// GPIO pins
const (
	GP0  Pin = GPIO0
	GP1  Pin = GPIO1
	GP2  Pin = GPIO2
	GP3  Pin = GPIO3
	GP4  Pin = GPIO4
	GP5  Pin = GPIO5
	GP6  Pin = GPIO6
	GP7  Pin = GPIO7
	GP8  Pin = GPIO8
	GP9  Pin = GPIO9
	GP10 Pin = GPIO10
	GP11 Pin = GPIO11
	GP12 Pin = GPIO12
	GP13 Pin = GPIO13
	GP14 Pin = GPIO14
	GP15 Pin = GPIO15
	GP16 Pin = GPIO16
	GP17 Pin = GPIO17
	GP18 Pin = GPIO18
	GP19 Pin = GPIO19
	GP20 Pin = GPIO20
	GP21 Pin = GPIO21
	GP22 Pin = GPIO22
	GP23 Pin = GPIO23
	GP24 Pin = GPIO24
	GP25 Pin = GPIO25
	GP26 Pin = GPIO26
	GP27 Pin = GPIO27
	GP28 Pin = GPIO28
	GP29 Pin = GPIO29
	GP30 Pin = GPIO30
	GP31 Pin = GPIO31
	GP32 Pin = GPIO32
	GP33 Pin = GPIO33
	GP34 Pin = GPIO34
	GP35 Pin = GPIO35
	GP36 Pin = GPIO36
	GP37 Pin = GPIO37
	GP38 Pin = GPIO38
	GP39 Pin = GPIO39
	GP40 Pin = GPIO40
	GP41 Pin = GPIO41
	GP42 Pin = GPIO42
	GP43 Pin = GPIO43
	GP44 Pin = GPIO44
	GP45 Pin = GPIO45
	GP46 Pin = GPIO46
	GP47 Pin = GPIO47

	// Onboard user LED (active high).
	LED Pin = GPIO14

	// Buttons and joystick (active high, pull-down).
	BUTTON_START   Pin = GPIO5
	BUTTON_SELECT  Pin = GPIO38
	BUTTON_A       Pin = GPIO6
	BUTTON_B       Pin = GPIO7
	JOYSTICK_UP    Pin = GPIO37
	JOYSTICK_DOWN  Pin = GPIO24
	JOYSTICK_LEFT  Pin = GPIO35
	JOYSTICK_RIGHT Pin = GPIO25
	JOYSTICK_CLICK Pin = GPIO36

	// LCD (SPI0) and backlight.
	LCD_CS        Pin = GPIO17
	LCD_SCK       Pin = GPIO18
	LCD_MOSI      Pin = GPIO19
	LCD_DC        Pin = GPIO21
	LCD_BACKLIGHT Pin = GPIO16

	// 12 MHz crystal on the SYCL Badge V2.
	xoscFreq = 12 // MHz
)

// No default I2C pins defined for this board.
const (
	I2C0_SDA_PIN = NoPin
	I2C0_SCL_PIN = NoPin
	I2C1_SDA_PIN = NoPin
	I2C1_SCL_PIN = NoPin
)

// SPI default pins (SPI0, shared with the LCD). The LCD is write-only: GPIO16
// is the backlight, so SPI0 has no MISO and SDI is NoPin.
const (
	SPI0_SCK_PIN = GPIO18
	SPI0_SDO_PIN = GPIO19
	SPI0_SDI_PIN = NoPin

	SPI1_SCK_PIN = GPIO10
	SPI1_SDO_PIN = GPIO11
	SPI1_SDI_PIN = GPIO12
)

// UART0 is routed to the debug connector (GPIO28 TX, GPIO29 RX), per the
// reference firmware's board_v2.zig.
const (
	UART0_TX_PIN = GPIO28
	UART0_RX_PIN = GPIO29
	UART1_TX_PIN = GPIO8
	UART1_RX_PIN = GPIO9
	UART_TX_PIN  = UART0_TX_PIN
	UART_RX_PIN  = UART0_RX_PIN
)

var DefaultUART = UART0

// USB identifiers (change these freely; they are cosmetic).
const (
	usb_STRING_PRODUCT      = "SYCL Badge V2"
	usb_STRING_MANUFACTURER = "SYCL"
)

var (
	usb_VID uint16 = 0x2E8A
	usb_PID uint16 = 0x000A
)
