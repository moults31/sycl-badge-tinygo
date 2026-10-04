//go:build tinygo

package cartridge

// onBaremetal is true for a real TinyGo firmware build and false for the host
// simulator and unit tests. The teaching carts use it to keep genuinely fatal
// demonstrations (a panic in a goroutine, heap exhaustion) real on the badge
// while degrading to a labelled simulation on the host, so a curious press in
// cmd/simui cannot kill the developer's sim process.
const onBaremetal = true
