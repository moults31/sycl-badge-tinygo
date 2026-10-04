package cartridge

// DefaultLibrary is the standard set of cartridges compiled into the firmware,
// in menu order. It is the single source of truth: the badge (main.go), the
// scripted host renderer (cmd/sim) and the interactive simulator (cmd/simui)
// all build on it, so a new cart only has to be added here. cmd/simui swaps the
// baked CARD SHOW for a factory backed by the user's live card library.
//
// CARD SHOW stays the hero: main.go boots straight into it with
// Runner.BootCart regardless of where it sits in the menu.
func DefaultLibrary() []Factory {
	return []Factory{
		{Name: "PLASMA", New: NewPlasma},
		{Name: "ZEROMAN", New: NewZeroman},
		{Name: "TASKS", New: NewTasks},
		{Name: "HEAP", New: NewHeap},
		{Name: "PANIC TEST", New: NewPanicTest},
		{Name: "CARD SHOW", New: NewCardShow},
	}
}
