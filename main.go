package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("boot: initializing display")
	lcdInit()
	fmt.Println("boot: display configured, drawing gopher")
	showGopher()
	fmt.Println("boot: gopher drawn")

	// The image is now static on the panel. Stay alive so the USB-CDC port
	// remains enumerated.
	for {
		time.Sleep(time.Second)
	}
}
