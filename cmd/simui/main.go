// Command simui runs the cartridge runtime in a local interactive window so the
// menu, the launch/exit chords, panic recovery, and every cart can be driven by
// hand and watched at 60 fps without hardware.
//
// It starts the very same cartridge.Runner the badge runs, at the same frame
// pace, and serves a small page on 127.0.0.1: the page renders the 160x128 panel
// and posts the state of its keyboard and on-screen controls back as the
// runner's Env. So the simulation is functionally the board's -- same carts,
// same edge-triggered menu, same 250 ms Start+Select exit chord, same recover
// loop -- only the panel and the pins are replaced.
//
// CARD SHOW renders the user's real cards. At startup the sim runs the very same
// tools/make_card.py the firmware build uses, on one shared card library, and
// loads the JSON it emits, so dropping in a card and relaunching the sim is
// enough -- no rebuild. The library is resolved as $SYCL_CARDS_DIR, else the
// main worktree's assets/cards (so every git worktree shares it), else
// ./assets/cards; -cards overrides it. With no local cards (or no Python) it
// falls back to the cards already baked into the binary.
//
//	go run ./cmd/simui                            # free port, opens a browser
//	go run ./cmd/simui -addr 127.0.0.1:8423 -open=false
//	go run ./cmd/simui -cards ~/my-cards
//
// There is no GUI-toolkit dependency: the window is the browser, and the only
// imports are the standard library and the hardware-free cartridge package.
package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sycl-badge-tinygo/cartridge"
)

//go:embed page.html
var pageHTML []byte

// Control bits. This is the wire format between the page and this server; it
// must match the BITS table in page.html.
const (
	bitStart  = 1 << 0
	bitSelect = 1 << 1
	bitA      = 1 << 2
	bitB      = 1 << 3
	bitClick  = 1 << 4
	bitUp     = 1 << 5
	bitDown   = 1 << 6
	bitLeft   = 1 << 7
	bitRight  = 1 << 8
)

const (
	frameWidth  = cartridge.Width
	frameHeight = cartridge.Height

	// One frame on the wire: a backlight byte followed by RGBA8888 pixels.
	frameBytes = 1 + frameWidth*frameHeight*4

	// framePace matches the runtime's ~60 fps target (make sim uses 16 ms too).
	framePace = 16 * time.Millisecond
)

// sim is the shared state between the runner (which paints frames and samples
// the controls) and the HTTP handlers (which stream frames and receive
// controls). The frame is kept as RGBA because that is what the canvas wants,
// and the backlight is a separate level so the page can dim the whole panel the
// way the PWM backlight does.
type sim struct {
	mu        sync.Mutex
	frame     []byte // RGBA8888, row-major, frameWidth*frameHeight*4 bytes
	backlight uint8

	buttons atomic.Uint32
}

func newSim() *sim {
	return &sim{frame: make([]byte, frameWidth*frameHeight*4), backlight: 255}
}

// Buttons implements cartridge.Env: the current level of every control.
func (s *sim) Buttons() cartridge.Buttons {
	m := s.buttons.Load()
	return cartridge.Buttons{
		Start:  m&bitStart != 0,
		Select: m&bitSelect != 0,
		A:      m&bitA != 0,
		B:      m&bitB != 0,
		Click:  m&bitClick != 0,
		Up:     m&bitUp != 0,
		Down:   m&bitDown != 0,
		Left:   m&bitLeft != 0,
		Right:  m&bitRight != 0,
	}
}

// present expands one RGB565 backbuffer into the shared RGBA frame. It is the
// display half of the runtime.
func (s *sim) present(f []uint16) {
	s.mu.Lock()
	buf := s.frame
	for i := 0; i < frameWidth*frameHeight && i < len(f); i++ {
		v := f[i]
		r := uint8(v>>11) & 0x1f
		g := uint8(v>>5) & 0x3f
		b := uint8(v) & 0x1f
		o := i * 4
		buf[o+0] = r<<3 | r>>2
		buf[o+1] = g<<2 | g>>4
		buf[o+2] = b<<3 | b>>2
		buf[o+3] = 0xff
	}
	s.mu.Unlock()
}

func (s *sim) setBacklight(level uint8) {
	s.mu.Lock()
	s.backlight = level
	s.mu.Unlock()
}

// snapshot copies the latest frame into pkt as "1 backlight byte + RGBA".
func (s *sim) snapshot(pkt []byte) {
	s.mu.Lock()
	pkt[0] = s.backlight
	copy(pkt[1:], s.frame)
	s.mu.Unlock()
}

// env is the cartridge.Env: a monotonic millisecond clock, a real frame-pacing
// sleep, and the controls the page posted.
type env struct {
	sim   *sim
	start time.Time
}

func (e *env) Buttons() cartridge.Buttons { return e.sim.Buttons() }
func (e *env) Millis() uint32             { return uint32(time.Since(e.start) / time.Millisecond) }
func (e *env) Sleep(ms uint32)            { time.Sleep(time.Duration(ms) * time.Millisecond) }

// display adapts sim to cartridge.Display.
type display struct{ sim *sim }

func (d display) Present(f []uint16)       { d.sim.present(f) }
func (d display) SetBacklight(level uint8) { d.sim.setBacklight(level) }

// handleFrames streams frames as a raw byte stream, one fixed-size packet per
// tick. A single long-lived response keeps latency low and avoids a request per
// frame; the page reads it incrementally with a stream reader.
func (s *sim) handleFrames(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	pkt := make([]byte, frameBytes)
	t := time.NewTicker(framePace)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			s.snapshot(pkt)
			if _, err := w.Write(pkt); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleFrame serves a single frame, the polling fallback for browsers whose
// fetch cannot expose a readable stream.
func (s *sim) handleFrame(w http.ResponseWriter, r *http.Request) {
	pkt := make([]byte, frameBytes)
	s.snapshot(pkt)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pkt)
}

// handleInput accepts the page's full control bitmask (b=) and stores it. The
// page sends one POST per change, not per frame.
func (s *sim) handleInput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	m := uint32(0)
	if v := r.URL.Query().Get("b"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			http.Error(w, "bad mask", http.StatusBadRequest)
			return
		}
		m = uint32(n)
	}
	s.buttons.Store(m)
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address; :0 picks a free port")
	open := flag.Bool("open", true, "open the sim in a browser window")
	cardsDir := flag.String("cards", "auto",
		`card folder or manifest to load at startup; "auto" = $SYCL_CARDS_DIR, else the main worktree's assets/cards, else ./assets/cards; "none" = baked cards`)
	python := flag.String("python", "python3", "Python interpreter for tools/make_card.py")
	flag.Parse()

	s := newSim()
	cardFactory, cardStatus := cardShowFactory(*cardsDir, *python)
	lib := []cartridge.Factory{
		{Name: "PLASMA", New: cartridge.NewPlasma},
		{Name: "ZEROMAN", New: cartridge.NewZeroman},
		{Name: "PANIC TEST", New: cartridge.NewPanicTest},
		cardFactory,
	}
	runner := cartridge.NewRunner(&env{sim: s, start: time.Now()}, display{s}, lib)
	runner.SetFrameMillis(uint32(framePace / time.Millisecond))
	runner.SetLogger(func(m string) { fmt.Println("sim:", m) })

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("simui: listen: %v", err)
	}
	url := "http://" + displayAddr(ln.Addr().String()) + "/"

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(pageHTML)
	})
	mux.HandleFunc("/frames", s.handleFrames)
	mux.HandleFunc("/frame.bin", s.handleFrame)
	mux.HandleFunc("/input", s.handleInput)

	fmt.Println("SYCL badge interactive sim")
	fmt.Printf("  window: %s\n", url)
	for _, line := range cardStatus {
		fmt.Println(line)
	}
	fmt.Println("  keys:   arrows/WASD move | Z A | X B | Enter start | Shift select | C stick click")
	fmt.Println("  cart:   hold Start+Select 250 ms to exit | Ctrl-C to stop")

	go runner.Run()

	if *open {
		if err := openBrowser(url); err != nil {
			fmt.Printf("  (couldn't open a browser: %v -- open %s)\n", err, url)
		}
	}

	if err := http.Serve(ln, mux); err != nil {
		log.Fatalf("simui: serve: %v", err)
	}
}

// displayAddr rewrites a wildcard listen address to something a browser can
// actually open.
func displayAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "::", "0.0.0.0":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// openBrowser best-effort opens url in the platform's default browser.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// cardShowFactory builds the CARD SHOW factory, preferring the user's local
// cards. It runs tools/make_card.py at startup -- the very same generator the
// firmware build uses -- so the sim shows the same real cards without a
// rebuild. With no local cards (or no Python), it falls back to the cards baked
// into the binary.
func cardShowFactory(dir, python string) (cartridge.Factory, []string) {
	baked := cartridge.Factory{Name: "CARD SHOW", New: cartridge.NewCardShow}

	if strings.TrimSpace(dir) == "" || dir == "none" {
		return baked, []string{"cards: using baked cards"}
	}
	if dir == "auto" {
		dir = sharedCardsDir()
	}
	manifest := resolveManifest(dir)
	if manifest == "" {
		return baked, []string{fmt.Sprintf("cards: no manifest in %s; using baked cards", dir)}
	}
	lib, err := bakeCards(manifest, python)
	if err != nil {
		return baked, []string{fmt.Sprintf("cards: %v; using baked cards", err)}
	}
	if len(lib) == 0 {
		return baked, []string{fmt.Sprintf("cards: %s produced no cards; using baked cards", manifest)}
	}

	lines := []string{fmt.Sprintf("cards: %d from %s (loaded live)", len(lib), manifest)}
	if names := cardNames(lib); names != "" {
		lines = append(lines, "       "+names)
	}
	return cartridge.Factory{
		Name: "CARD SHOW",
		New:  func() cartridge.Cartridge { return cartridge.NewCardShowWith(lib) },
	}, lines
}

// resolveManifest interprets the -cards argument: a *.json path is the manifest
// itself; otherwise it is a folder expected to contain manifest.json. A
// relative folder not found under the current directory is searched for up the
// tree, so the sim also works when started from a subdirectory.
func resolveManifest(arg string) string {
	if strings.HasSuffix(arg, ".json") {
		if fileExists(arg) {
			return arg
		}
		return ""
	}
	if local := filepath.Join(arg, "manifest.json"); fileExists(local) {
		return local
	}
	if !filepath.IsAbs(arg) {
		return findUp(filepath.Join(arg, "manifest.json"))
	}
	return ""
}

// bakeCards runs tools/make_card.py on manifest and decodes its JSON output.
func bakeCards(manifest, python string) ([]cartridge.CardAsset, error) {
	script := findMakeCard()
	if script == "" {
		return nil, fmt.Errorf("tools/make_card.py not found")
	}
	cmd := exec.Command(python, script, "--manifest", manifest, "--format", "json", "-o", "-")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.Join(strings.Fields(stderr.String()), " ")
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("make_card.py: %s", msg)
	}
	if warn := strings.TrimSpace(stderr.String()); warn != "" {
		// Surface generator warnings (e.g. "skipping missing ..." for manifest
		// entries with no image), but not its "wrote N cards" summary line.
		for _, line := range strings.Split(warn, "\n") {
			if strings.HasPrefix(line, "make_card: wrote") {
				continue
			}
			fmt.Fprintln(os.Stderr, line)
		}
	}
	return decodeCards(out)
}

// cardDoc mirrors the JSON tools/make_card.py --format json emits.
type cardDoc struct {
	Cards []struct {
		Name    string   `json:"name"`
		Set     string   `json:"set"`
		Types   []string `json:"types"`
		Rarity  string   `json:"rarity"`
		MaskW   int      `json:"mask_w"`
		MaskH   int      `json:"mask_h"`
		MaskB64 string   `json:"mask_b64"`
		Palette [][3]int `json:"palette"`
	} `json:"cards"`
}

// decodeCards turns the generator's JSON into cartridge.CardAsset values.
func decodeCards(data []byte) ([]cartridge.CardAsset, error) {
	var doc cardDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse generator output: %w", err)
	}
	lib := make([]cartridge.CardAsset, 0, len(doc.Cards))
	for _, c := range doc.Cards {
		mask, err := base64.StdEncoding.DecodeString(c.MaskB64)
		if err != nil {
			return nil, fmt.Errorf("card %q: mask: %w", c.Name, err)
		}
		pal := make([]uint16, len(c.Palette))
		for i, p := range c.Palette {
			pal[i] = cartridge.RGB565(uint8(p[0]), uint8(p[1]), uint8(p[2]))
		}
		lib = append(lib, cartridge.CardAsset{
			Name:    c.Name,
			Set:     c.Set,
			Types:   c.Types,
			Rarity:  c.Rarity,
			MaskW:   c.MaskW,
			MaskH:   c.MaskH,
			Mask:    mask,
			Palette: pal,
		})
	}
	return lib, nil
}

// sharedCardsDir mirrors tools/cards_dir.sh so a directly-run sim finds the
// same shared library the Makefile uses: $SYCL_CARDS_DIR, else the main
// worktree's assets/cards, else the current worktree's.
func sharedCardsDir() string {
	if d := strings.TrimSpace(os.Getenv("SYCL_CARDS_DIR")); d != "" {
		return d
	}
	if main := mainWorktreeRoot(); main != "" {
		if p := filepath.Join(main, "assets", "cards"); dirExists(p) {
			return p
		}
	}
	return filepath.Join("assets", "cards")
}

// mainWorktreeRoot returns the main checkout that owns the git common dir, so
// every linked worktree resolves to that one card library.
func mainWorktreeRoot() string {
	if out, err := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir").Output(); err == nil {
		return filepath.Dir(strings.TrimSpace(string(out)))
	}
	out, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		p = filepath.Join(wd, p)
	}
	return filepath.Dir(p)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// findMakeCard locates tools/make_card.py: from the current worktree first, so
// a feature branch can run a newer generator, then from the main worktree (where
// the shared card library lives), so a stale or minimal worktree still works.
func findMakeCard() string {
	if p := findUp(filepath.Join("tools", "make_card.py")); p != "" {
		return p
	}
	if main := mainWorktreeRoot(); main != "" {
		if p := filepath.Join(main, "tools", "make_card.py"); fileExists(p) {
			return p
		}
	}
	return ""
}

// findUp walks up from the working directory looking for rel.
func findUp(rel string) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if p := filepath.Join(dir, rel); fileExists(p) {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func cardNames(lib []cartridge.CardAsset) string {
	names := make([]string, len(lib))
	for i, c := range lib {
		names[i] = c.Name
	}
	s := strings.Join(names, ", ")
	if len(s) > 100 {
		s = s[:97] + "..."
	}
	return s
}
