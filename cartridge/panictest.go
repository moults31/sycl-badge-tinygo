package cartridge

// PanicTest is a phase-1 diagnostic cartridge: it launches cleanly, then
// panics on its first Update. It exists so the recover path can be exercised
// on real hardware and in the simulator; phase 2 replaces its menu slot with
// zeroman.
type PanicTest struct{}

// NewPanicTest returns the panic-test cartridge.
func NewPanicTest() Cartridge { return PanicTest{} }

// Name implements Cartridge.
func (PanicTest) Name() string { return "PANIC TEST" }

// Start paints a dim frame, then Update panics.
func (PanicTest) Start(p *Platform) { p.clear(RGB565(0x20, 0x00, 0x00)) }

// Update always panics, to prove the runtime recovers back to the menu.
func (PanicTest) Update(p *Platform) { panic("forced panic") }
