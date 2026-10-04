package main

import (
	"fmt"

	"sycl-badge-tinygo/cartridge"
)

func main() {
	fmt.Println("boot: initializing display")
	lcdInit()
	fmt.Println("boot: display ready, starting cartridge runtime")

	runner := cartridge.NewRunner(selectEnv(), lcdDisplay{}, cartridge.DefaultLibrary())
	runner.SetLogger(func(m string) { fmt.Println(m) })
	// The badge comes up in CARD SHOW; Start+Select exits to the cart menu.
	runner.BootCart("CARD SHOW")
	runner.Run()
}
