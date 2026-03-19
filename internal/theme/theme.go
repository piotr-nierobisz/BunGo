package theme

import "github.com/charmbracelet/lipgloss"

var (
	// Primary is a magenta-pink color, ensuring high contrast on both dark and light modes.
	Primary = lipgloss.AdaptiveColor{Light: "#D111D1", Dark: "#FF00FF"}

	// Secondary is a cyan-blue.
	Secondary = lipgloss.AdaptiveColor{Light: "#007BA7", Dark: "#00FFFF"}

	// Text is the standard text color.
	Text = lipgloss.AdaptiveColor{Light: "#111111", Dark: "#EEEEEE"}

	// Muted is for secondary information, like commands or paths.
	Muted = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#A0A0A0"}

	// Border is used for box borders.
	Border = lipgloss.AdaptiveColor{Light: "#D9D9D9", Dark: "#2A2A32"}

	// Success is a green color for success messages.
	Success = lipgloss.AdaptiveColor{Light: "#008800", Dark: "#00FF00"}

	// Accent is gold/yellow for spinners and warnings.
	Accent = lipgloss.AdaptiveColor{Light: "#D4AF37", Dark: "#FFD700"}

	// Highlight background
	HighlightBg = lipgloss.AdaptiveColor{Light: "#F0F0F0", Dark: "#222222"}
)
