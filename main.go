package main

import (
	"fmt"
	"machine"
	"time"
)

const led = machine.GPIO14

func main() {
	led.Configure(machine.PinConfig{Mode: machine.PinOutput})
	n := 0
	for {
		led.High()
		fmt.Printf("hello from SYCL Badge V2 #%d\r\n", n)
		time.Sleep(time.Millisecond * 250)
		led.Low()
		time.Sleep(time.Millisecond * 250)
		n++
	}
}
