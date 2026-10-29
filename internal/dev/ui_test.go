package dev

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// plain strips ANSI styling so assertions do not depend on the detected color profile.
func plain(s string) string { return ansiEscape.ReplaceAllString(s, "") }

func TestRenderHeader_showsProjectMetadata(t *testing.T) {
	t.Parallel()
	ui := theme.EN.Dev.UI
	h := plain(renderHeader("/home/dev/shop", "./cmd/shop", "v1.4.2"))

	for _, want := range []string{
		ui.Title,
		strings.SplitN(ui.Description, "\n", 2)[0],
		ui.LabelBunGoVersion + "v1.4.2",
		ui.LabelReactRuntime + theme.EmbeddedReactVersion,
		ui.LabelProjectRoot + "/home/dev/shop",
		ui.LabelRunTarget + "./cmd/shop",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("header missing %q:\n%s", want, h)
		}
	}
	// The banner's leading newline is trimmed so the art starts on the first line.
	firstBannerLine := strings.Split(strings.TrimPrefix(ui.ASCIIBanner, "\n"), "\n")[0]
	if got := strings.Split(h, "\n")[0]; !strings.Contains(got, strings.TrimSpace(firstBannerLine)) {
		t.Errorf("first header line = %q; want banner art", got)
	}
}

func TestRenderFooter_states(t *testing.T) {
	t.Parallel()
	ui := theme.EN.Dev.UI

	active := plain(renderFooter("⣾", false, 0))
	for _, want := range []string{ui.FooterWatchingText + " ⣾", ui.FooterPressCtrlC} {
		if !strings.Contains(active, want) {
			t.Errorf("active footer missing %q:\n%s", want, active)
		}
	}
	if strings.Contains(active, ui.FooterShuttingDown) {
		t.Error("active footer must not announce shutdown")
	}

	quitting := plain(renderFooter("⣾", true, 0))
	if !strings.Contains(quitting, ui.FooterShuttingDown) {
		t.Errorf("quitting footer missing shutdown text:\n%s", quitting)
	}
	if strings.Contains(quitting, ui.FooterWatchingText) || strings.Contains(quitting, ui.FooterPressCtrlC) {
		t.Errorf("quitting footer should only show the shutdown status:\n%s", quitting)
	}
}

func TestRenderFooter_matchesHeaderWidth(t *testing.T) {
	t.Parallel()
	natural := renderFooter("⣾", false, 0)
	matched := renderFooter("⣾", false, 150)
	if w := lipgloss.Width(natural); w >= 150 {
		t.Fatalf("natural footer width = %d; test needs it narrower than 150", w)
	}
	if w := lipgloss.Width(matched); w != 150 {
		t.Fatalf("matched footer width = %d; want 150", w)
	}
	// The top border spans the full requested width so it lines up with the header rule.
	if !strings.Contains(plain(matched), strings.Repeat("─", 150)) {
		t.Fatalf("footer border does not span 150 columns:\n%s", plain(matched))
	}
}

func newTestDevModel() devModel {
	return newDevModel("/home/dev/shop", "./cmd/shop", "v1.4.2")
}

// reservedRows is the number of terminal rows used by header and footer.
func reservedRows(m devModel) int {
	return lipgloss.Height(m.header) + lipgloss.Height(renderFooter(m.spinner.View(), m.quitting, m.headerWidth))
}

func update(t *testing.T, m devModel, msg tea.Msg) (devModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	dm, ok := next.(devModel)
	if !ok {
		t.Fatalf("Update returned %T; want devModel", next)
	}
	return dm, cmd
}

func TestDevModel_initialState(t *testing.T) {
	t.Parallel()
	m := newTestDevModel()
	if got := m.View(); got != theme.EN.Dev.UI.Initializing {
		t.Fatalf("View before first WindowSizeMsg = %q; want initializing text", got)
	}
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should start the spinner")
	}
	if _, ok := cmd().(spinner.TickMsg); !ok {
		t.Fatal("Init command should produce a spinner tick")
	}
}

func TestDevModel_windowSizeLaysOutViewport(t *testing.T) {
	t.Parallel()
	m, _ := update(t, newTestDevModel(), tea.WindowSizeMsg{Width: 140, Height: 50})
	if !m.ready {
		t.Fatal("model should be ready after the first WindowSizeMsg")
	}
	reserved := reservedRows(m)
	if m.viewport.Width != 140 || m.viewport.Height != 50-reserved {
		t.Fatalf("viewport = %dx%d; want 140x%d", m.viewport.Width, m.viewport.Height, 50-reserved)
	}
	if m.viewport.YPosition != lipgloss.Height(m.header) {
		t.Fatalf("viewport YPosition = %d; want header height %d", m.viewport.YPosition, lipgloss.Height(m.header))
	}
	view := m.View()
	if h := lipgloss.Height(view); h != 50 {
		t.Fatalf("view height = %d; want it to fill the 50-row terminal", h)
	}
	if !strings.Contains(plain(view), theme.EN.Dev.UI.Title) || !strings.Contains(plain(view), theme.EN.Dev.UI.FooterPressCtrlC) {
		t.Fatalf("view should contain header and footer:\n%s", plain(view))
	}

	m, _ = update(t, m, logMsg("still here"))
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.viewport.Width != 120 || m.viewport.Height != 40-reserved {
		t.Fatalf("resized viewport = %dx%d; want 120x%d", m.viewport.Width, m.viewport.Height, 40-reserved)
	}
	if lipgloss.Height(m.View()) != 40 || !strings.Contains(m.View(), "still here") {
		t.Fatal("resize should keep the logs and refit the view")
	}
}

func TestDevModel_logsReceivedBeforeSizeAreKept(t *testing.T) {
	t.Parallel()
	m := newTestDevModel()
	m, _ = update(t, m, logMsg("booting app\n"))
	m, _ = update(t, m, logMsg("\n")) // bare newline: nothing to show
	m, _ = update(t, m, logMsg(""))
	if len(m.logs) != 1 || m.logs[0] != "booting app" {
		t.Fatalf("logs = %q; want trimmed single entry", m.logs)
	}
	if m.View() != theme.EN.Dev.UI.Initializing {
		t.Fatal("logs must not render before the terminal size is known")
	}

	m, _ = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 50})
	if !strings.Contains(m.View(), "booting app") {
		t.Fatalf("early log missing once ready:\n%s", plain(m.View()))
	}
}

func TestDevModel_followsNewestLogsAndForwardsScrollKeys(t *testing.T) {
	t.Parallel()
	m := newTestDevModel()
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 140, Height: reservedRows(m) + 3}) // 3 log rows
	for i := 0; i < 10; i++ {
		m, _ = update(t, m, logMsg(fmt.Sprintf("line %02d\n", i)))
	}
	view := m.viewport.View()
	if !strings.Contains(view, "line 09") || strings.Contains(view, "line 06") || !m.viewport.AtBottom() {
		t.Fatalf("viewport should follow the newest logs:\n%s", view)
	}

	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyUp})
	view = m.viewport.View()
	if !strings.Contains(view, "line 06") || strings.Contains(view, "line 09") {
		t.Fatalf("up key should scroll the log viewport by one line:\n%s", view)
	}

	// Growing the terminal while scrolled up must not leave the offset past
	// the end of the logs (which would render blank rows).
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 140, Height: reservedRows(m) + 20})
	if view = m.viewport.View(); !strings.Contains(view, "line 00") || !strings.Contains(view, "line 09") {
		t.Fatalf("taller viewport should show every line:\n%s", view)
	}

	m, _ = update(t, m, logMsg("line 10"))
	if view = m.viewport.View(); !strings.Contains(view, "line 10") {
		t.Fatalf("new log should jump back to the bottom:\n%s", view)
	}
}

// TestDevModel_shortTerminalDoesNotPanic is a regression test: on a terminal
// shorter than header+footer the viewport height went negative and the next
// log line panicked with "slice bounds out of range".
func TestDevModel_shortTerminalDoesNotPanic(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on a short terminal: %v", r)
		}
	}()

	m := newTestDevModel()
	short := reservedRows(m) - 5 // e.g. a 19-row split pane
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 80, Height: short})
	if m.viewport.Height != 0 {
		t.Fatalf("viewport height = %d; want 0 when there is no room for logs", m.viewport.Height)
	}
	for i := 0; i < 3; i++ {
		m, _ = update(t, m, logMsg(fmt.Sprintf("line %d\n", i)))
		_ = m.View()
	}

	// Growing the terminal again (the resize branch) brings the logs back.
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 80, Height: short - 3})
	if m.viewport.Height != 0 {
		t.Fatalf("resized viewport height = %d; want 0", m.viewport.Height)
	}
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 80, Height: reservedRows(m) + 3})
	if view := m.viewport.View(); !strings.Contains(view, "line 2") {
		t.Fatalf("logs should show once the terminal is tall enough:\n%s", view)
	}
}

func TestDevModel_quitMessages(t *testing.T) {
	t.Parallel()
	for name, msg := range map[string]tea.Msg{
		"ctrl+c":   tea.KeyMsg{Type: tea.KeyCtrlC},
		"dev done": devDoneMsg{},
	} {
		t.Run(name, func(t *testing.T) {
			m := newTestDevModel()
			m, _ = update(t, m, tea.WindowSizeMsg{Width: 140, Height: 50})
			m, cmd := update(t, m, msg)
			if !m.quitting {
				t.Fatal("model should be quitting")
			}
			if cmd == nil {
				t.Fatal("expected tea.Quit")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("command should be tea.Quit")
			}
			if !strings.Contains(plain(m.View()), theme.EN.Dev.UI.FooterShuttingDown) {
				t.Fatalf("view should show shutdown status:\n%s", plain(m.View()))
			}
		})
	}

	m, _ := update(t, newTestDevModel(), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.quitting {
		t.Fatal("only Ctrl+C quits; other keys must not")
	}
}

func TestDevModel_spinnerTickAdvancesFrame(t *testing.T) {
	t.Parallel()
	m := newTestDevModel()
	before := m.spinner.View()
	m, cmd := update(t, m, m.Init()())
	if m.spinner.View() == before {
		t.Fatal("spinner frame should advance on its tick")
	}
	if cmd == nil {
		t.Fatal("tick should schedule the next frame")
	}
}

// captureStdout redirects os.Stdout while fn runs and returns what was printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	func() {
		defer func() { os.Stdout = orig }()
		fn()
	}()
	_ = w.Close()
	out, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestUIWriter_withoutProgramPrintsCompleteLines(t *testing.T) {
	out := captureStdout(t, func() {
		w := &uiWriter{}
		for _, chunk := range []string{"alpha\nbe", "ta\n", "partial"} {
			if n, err := io.WriteString(w, chunk); err != nil || n != len(chunk) {
				t.Errorf("Write(%q) = %d, %v", chunk, n, err)
			}
		}
	})
	if out != "alpha\nbeta\n" {
		t.Fatalf("stdout = %q; want only completed lines", out)
	}
}

// TestUIWriter_feedsDevModelThroughProgram runs the real dev model in a
// headless Bubble Tea program and drives it the way RunUI does.
func TestUIWriter_feedsDevModelThroughProgram(t *testing.T) {
	t.Parallel()
	out := &syncBuffer{}
	p := tea.NewProgram(newTestDevModel(),
		tea.WithInput(nil),
		tea.WithOutput(out),
		tea.WithoutSignalHandler(),
	)
	type result struct {
		model tea.Model
		err   error
	}
	done := make(chan result, 1)
	go func() {
		m, err := p.Run()
		done <- result{m, err}
	}()

	p.Send(tea.WindowSizeMsg{Width: 160, Height: 60})
	w := &uiWriter{p: p}

	// stdout and stderr share one writer in RunUI; whole-line writes from
	// concurrent goroutines must arrive intact.
	const writers, linesEach = 4, 25
	var wg sync.WaitGroup
	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < linesEach; i++ {
				fmt.Fprintf(w, "writer %d line %02d\n", g, i)
			}
		}(g)
	}
	wg.Wait()
	io.WriteString(w, "split ")
	io.WriteString(w, "line\n\n")
	io.WriteString(w, "never terminated")
	p.Send(devDoneMsg{})

	var r result
	select {
	case r = <-done:
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("program did not quit after devDoneMsg")
	}
	if r.err != nil {
		t.Fatalf("Run: %v", r.err)
	}
	final := r.model.(devModel)
	if !final.quitting {
		t.Fatal("final model should be quitting")
	}
	if got, want := len(final.logs), writers*linesEach+1; got != want {
		t.Fatalf("got %d log lines; want %d", got, want)
	}
	lineRe := regexp.MustCompile(`^writer \d line \d\d$`)
	for _, l := range final.logs[:writers*linesEach] {
		if !lineRe.MatchString(l) {
			t.Fatalf("garbled log line %q", l)
		}
	}
	if last := final.logs[len(final.logs)-1]; last != "split line" {
		t.Fatalf("last log = %q; want %q", last, "split line")
	}
	rendered := plain(out.String())
	if !strings.Contains(rendered, "split line") || !strings.Contains(rendered, theme.EN.Dev.UI.FooterShuttingDown) {
		t.Fatalf("rendered output missing final frame:\n%s", rendered)
	}
}

// RunUI tests launch a fake app through /bin/sh and live in ui_unix_test.go.
