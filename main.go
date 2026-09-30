package main

import (
	"fmt"

	"sycl-badge-tinygo/cartridge"
)

func main() {
	fmt.Println("boot: initializing display")
	lcdInit()
	fmt.Println("boot: display ready, starting cartridge runtime")

	lib := []cartridge.Factory{
		{Name: "PLASMA", New: cartridge.NewPlasma},
		{Name: "ZEROMAN", New: cartridge.NewZeroman},
		{Name: "PANIC TEST", New: cartridge.NewPanicTest},
		{Name: "CARD SHOW", New: cartridge.NewCardShow},
	}
	runner := cartridge.NewRunner(selectEnv(), lcdDisplay{}, lib)
	runner.SetLogger(func(m string) { fmt.Println(m) })
	runner.Run()
}
