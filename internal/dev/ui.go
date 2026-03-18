package dev

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	bungo "github.com/piotr-nierobisz/BunGo"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF00FF")).
			Background(lipgloss.Color("#222222")).
			Padding(1, 2)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FFFF")).
			Italic(true).
			MarginBottom(1)

	infoItemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#A0A0A0"))
	infoValStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)

	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Bold(true)
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
	logStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Italic(true)
)

const epicAscii = `
  ___             ___      
 | _ )_  _ _ _   / __|___  
 | _ \ || | ' \ | (_ / _ \ 
 |___/\_,_|_||_| \___\___/ 
`

func renderEpicHeader(root, entry, cliVersion string) string {
	var b strings.Builder

	coloredAscii := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF")).Render(strings.TrimPrefix(epicAscii, "\n"))
	b.WriteString(coloredAscii + "\n")

	b.WriteString(titleStyle.Render("=== BunGo Dev Environment ===") + "\n")
	b.WriteString(subtitleStyle.Render("Crafting the future of Go web apps...") + "\n")

	b.WriteString(infoItemStyle.Render("BunGo version: ") + infoValStyle.Render(cliVersion) + "\n")
	b.WriteString(infoItemStyle.Render("React runtime: ") + infoValStyle.Render(bungo.EmbeddedReactVersion) + "\n")
	b.WriteString(infoItemStyle.Render("Project root : ") + infoValStyle.Render(root) + "\n")
	b.WriteString(infoItemStyle.Render("Run target   : ") + infoValStyle.Render(entry) + "\n\n")

	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render("Press Ctrl+C to stop.") + "\n\n")

	return b.String()
}

type logMsg string
type devDoneMsg struct{}

type devModel struct {
	spinner     spinner.Model
	quitting    bool
	header      string
	lastLogLine string
}

func newDevModel(root, entry, cliVersion string) devModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	return devModel{
		spinner: s,
		header:  renderEpicHeader(root, entry, cliVersion),
	}
}

func (m devModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m devModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			return m, tea.Quit
		}
	case devDoneMsg:
		m.quitting = true
		return m, tea.Quit
	case logMsg:
		m.lastLogLine = string(msg)
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m devModel) View() string {
	if m.quitting {
		return m.header + lipgloss.NewStyle().Foreground(lipgloss.Color("#A0A0A0")).Render("Shutting down BunGo dev server...") + "\n"
	}
	spin := m.spinner.View()
	text := statusStyle.Render("Server is continuously working. Watching for changes...")

	logLine := ""
	if m.lastLogLine != "" {
		logLine = "\n" + logStyle.Render(m.lastLogLine)
	}

	return fmt.Sprintf("%s%s %s%s", m.header, spin, text, logLine)
}

// uiWriter buffers lines and sends them as logMsg
type uiWriter struct {
	p   *tea.Program
	buf bytes.Buffer
	mu  sync.Mutex
}

func (w *uiWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, ch := range b {
		if ch == '\n' {
			if w.p != nil {
				w.p.Send(logMsg(w.buf.String()))
			} else {
				fmt.Println(w.buf.String())
			}
			w.buf.Reset()
		} else {
			w.buf.WriteByte(ch)
		}
	}

	return len(b), nil
}

func RunUI(ctx context.Context, stop context.CancelFunc, root, entry, cliVersion string) error {
	uiW := &uiWriter{}

	errCh := make(chan error, 1)

	p := tea.NewProgram(newDevModel(root, entry, cliVersion))
	uiW.p = p

	go func() {
		errCh <- Run(ctx, root, Options{
			RunTarget: entry,
			Stdout:    uiW,
			Stderr:    uiW,
		})
		p.Send(devDoneMsg{})
	}()

	if _, err := p.Run(); err != nil {
		stop()
		<-errCh
		return err
	}

	stop()
	return <-errCh
}
