package dev

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/piotr-nierobisz/BunGo/internal/theme"
)

var (
	headerStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(theme.Border).
			PaddingBottom(1).
			MarginBottom(1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#000000"}).
			Background(theme.Primary).
			Padding(0, 2)

	infoItemStyle = lipgloss.NewStyle().Foreground(theme.Muted)
	infoValStyle  = lipgloss.NewStyle().Foreground(theme.Text).Bold(true)

	spinnerStyle = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
	statusStyle  = lipgloss.NewStyle().Foreground(theme.Text)

	footerStyle = lipgloss.NewStyle().
			MarginTop(1).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(theme.Border).
			PaddingTop(1)
)

// renderHeader builds the static top section of the BunGo dev TUI.
// Inputs:
// - root: absolute project root path shown in the info section.
// - entry: go run target shown in the info section.
// - cliVersion: BunGo CLI version string shown in the info section.
// Outputs:
// - string: rendered header block including banner, metadata, and styling.
func renderHeader(root, entry, cliVersion string) string {
	var b strings.Builder

	ascii := theme.EN.Dev.UI.ASCIIBanner
	coloredAscii := lipgloss.NewStyle().Foreground(theme.Primary).Bold(true).Render(strings.TrimPrefix(ascii, "\n"))
	b.WriteString(coloredAscii + "\n")

	b.WriteString(titleStyle.Render(theme.EN.Dev.UI.Title) + "\n")

	b.WriteString(lipgloss.NewStyle().Foreground(theme.Muted).Render(theme.EN.Dev.UI.Description) + "\n\n")

	b.WriteString(infoItemStyle.Render(theme.EN.Dev.UI.LabelBunGoVersion) + infoValStyle.Render(cliVersion) + "\n")
	b.WriteString(infoItemStyle.Render(theme.EN.Dev.UI.LabelReactRuntime) + infoValStyle.Render(theme.EmbeddedReactVersion) + "\n")
	b.WriteString(infoItemStyle.Render(theme.EN.Dev.UI.LabelProjectRoot) + infoValStyle.Render(root) + "\n")
	b.WriteString(infoItemStyle.Render(theme.EN.Dev.UI.LabelRunTarget) + infoValStyle.Render(entry))

	return headerStyle.Render(b.String())
}

// renderFooter builds the footer status line for active or quitting TUI states.
// Inputs:
// - spin: spinner frame string rendered in the watching status line.
// - quitting: true when the UI is shutting down and should show shutdown status.
// - matchWidth: optional width to align footer border with the rendered header width.
// Outputs:
// - string: styled footer block rendered for the current UI state.
func renderFooter(spin string, quitting bool, matchWidth int) string {
	var b strings.Builder
	if quitting {
		b.WriteString(statusStyle.Render(theme.EN.Dev.UI.FooterShuttingDown))
	} else {
		b.WriteString(fmt.Sprintf(theme.EN.Dev.UI.FooterWatchingLineFmt, statusStyle.Render(theme.EN.Dev.UI.FooterWatchingText), spin))
		b.WriteString(lipgloss.NewStyle().Foreground(theme.Muted).Render(theme.EN.Dev.UI.FooterPressCtrlC))
	}
	if matchWidth > 0 {
		return footerStyle.Width(matchWidth).Render(b.String())
	}
	return footerStyle.Render(b.String())
}

type logMsg string
type devDoneMsg struct{}

type devModel struct {
	spinner     spinner.Model
	viewport    viewport.Model
	quitting    bool
	header      string
	headerWidth int
	logs        []string
	ready       bool
}

// newDevModel creates the initial Bubble Tea model used by the dev TUI.
// Inputs:
// - root: absolute project root path displayed in header metadata.
// - entry: go run target displayed in header metadata.
// - cliVersion: BunGo CLI version displayed in header metadata.
// Outputs:
// - devModel: initialized model with spinner, header, and empty log buffer.
func newDevModel(root, entry, cliVersion string) devModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle
	h := renderHeader(root, entry, cliVersion)
	return devModel{
		spinner:     s,
		header:      h,
		headerWidth: lipgloss.Width(h),
		logs:        make([]string, 0),
	}
}

// Init returns the initial Bubble Tea command for the dev model.
// Inputs:
// - none
// Outputs:
// - tea.Cmd: spinner tick command used to animate the status indicator.
func (m devModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update applies Bubble Tea messages to model state and returns follow-up commands.
// Inputs:
// - msg: Bubble Tea message representing input, resize, tick, or app lifecycle events.
// Outputs:
// - tea.Model: updated model state after applying the incoming message.
// - tea.Cmd: command batch scheduling subsequent Bubble Tea updates.
func (m devModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(m.header)
		footerHeight := lipgloss.Height(renderFooter(m.spinner.View(), m.quitting, m.headerWidth))
		verticalMarginHeight := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = headerHeight
			// Initialize with logs if there are already any before window size is known
			m.viewport.SetContent(strings.Join(m.logs, "\n"))
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
		}

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			return m, tea.Quit
		}

	case devDoneMsg:
		m.quitting = true
		return m, tea.Quit

	case logMsg:
		// Clean up msg string
		cleanMsg := strings.TrimRight(string(msg), "\n")
		// Sometimes an empty log string is sent if just a newline was processed
		if cleanMsg != "" {
			m.logs = append(m.logs, cleanMsg)
			m.viewport.SetContent(strings.Join(m.logs, "\n"))
			m.viewport.GotoBottom()
		}

	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the full Bubble Tea screen for the current model state.
// Inputs:
// - none
// Outputs:
// - string: full terminal view containing header, log viewport, and footer.
func (m devModel) View() string {
	if !m.ready {
		return theme.EN.Dev.UI.Initializing
	}

	// Create the view
	return fmt.Sprintf("%s\n%s\n%s", m.header, m.viewport.View(), renderFooter(m.spinner.View(), m.quitting, m.headerWidth))
}

// uiWriter buffers lines and sends them as logMsg
type uiWriter struct {
	p   *tea.Program
	buf bytes.Buffer
	mu  sync.Mutex
}

// Write appends bytes to a line buffer and forwards completed lines to the TUI.
// Inputs:
// - b: byte chunk written by background dev process output streams.
// Outputs:
// - int: number of bytes consumed from b.
// - error: non-nil when downstream logging dispatch fails.
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

// RunUI starts the Bubble Tea dev UI and runs the underlying dev process loop.
// Inputs:
// - ctx: cancellation context propagated to the underlying dev runner.
// - stop: cancellation function called when UI exits or errors.
// - root: project root path passed to Run and shown in UI metadata.
// - entry: go run target passed to Run and shown in UI metadata.
// - cliVersion: BunGo CLI version shown in UI metadata.
// Outputs:
// - error: non-nil when UI runtime or background dev runner fails.
func RunUI(ctx context.Context, stop context.CancelFunc, root, entry, cliVersion string) error {
	uiW := &uiWriter{}

	errCh := make(chan error, 1)

	// Use alternate screen to render full terminal
	p := tea.NewProgram(newDevModel(root, entry, cliVersion), tea.WithAltScreen(), tea.WithMouseCellMotion())
	uiW.p = p

	go func() {
		errCh <- Run(ctx, root, &Options{
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
