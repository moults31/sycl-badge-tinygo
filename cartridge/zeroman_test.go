package cartridge

import "testing"

func zeromanPlatform() *Platform {
	return &Platform{frame: make([]uint16, Width*Height)}
}

func TestZeromanTitleToPlaying(t *testing.T) {
	p := zeromanPlatform()
	z := &zeroman{}
	z.Start(p)

	// No input: the title screen persists.
	for i := 0; i < 120; i++ {
		z.Update(p)
	}
	if z.state != zTitle {
		t.Fatalf("expected title with no input, got %v", z.state)
	}

	// Any key leaves the title; then the start logo, then playing.
	p.buttons = Buttons{Up: true}
	z.Update(p)
	p.buttons = Buttons{}
	for i := 0; i < 200; i++ {
		z.Update(p)
	}
	if z.state != zPlaying {
		t.Fatalf("expected playing, got %v", z.state)
	}
}

func TestZeromanPlayerMoves(t *testing.T) {
	p := zeromanPlatform()
	z := &zeroman{}
	z.Start(p)

	p.buttons = Buttons{Up: true}
	z.Update(p)
	p.buttons = Buttons{}
	for i := 0; i < 200; i++ {
		z.Update(p)
	}
	if z.state != zPlaying {
		t.Fatalf("did not reach playing, state=%v", z.state)
	}

	x0 := z.player.box.x
	p.buttons = Buttons{Right: true}
	for i := 0; i < 40; i++ {
		z.Update(p)
	}
	if z.player.box.x <= x0 {
		t.Fatalf("player did not move right: %d -> %d", x0, z.player.box.x)
	}
	if z.player.state != psRunning {
		t.Fatalf("expected running, got %v", z.player.state)
	}
}

func TestZeromanFrameIsPainted(t *testing.T) {
	p := zeromanPlatform()
	z := &zeroman{}
	z.Start(p)
	p.buttons = Buttons{Up: true}
	z.Update(p)

	nonBlack := 0
	for _, c := range p.frame {
		if c != 0 {
			nonBlack++
		}
	}
	if nonBlack == 0 {
		t.Fatal("title frame is entirely black")
	}
}
