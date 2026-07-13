package tui

import (
	"github.com/charmbracelet/lipgloss"

	"shelter-cli/internal/shelter"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF9C")).
			Align(lipgloss.Center)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#00FF9C")).
			Padding(1, 3)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Italic(true)

	okStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#00FF9C"))

	failStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF5F5F"))

	checkLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D7D7D")).
			Width(14)

	checkTargetStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Width(16)

	fieldLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D7D7D")).
			Width(10)
)

func (m Model) View() string {
	if m.screen == screenSetup {
		return m.viewSetup()
	}
	return m.viewMain()
}

func (m Model) viewSetup() string {
	labels := []string{"DNS 1", "DNS 2", "DNS Key"}
	lines := []string{
		titleStyle.Width(40).Render("SETUP"),
		"",
	}
	for i, in := range m.inputs {
		lines = append(lines, fieldLabelStyle.Render(labels[i]+":")+" "+in.View())
	}
	lines = append(lines, "")
	if m.saveErr != "" {
		lines = append(lines, failStyle.Render(m.saveErr))
	} else if m.allFilled() {
		lines = append(lines, okStyle.Render("enter to start"))
	} else {
		lines = append(lines, helpStyle.Render("fill all fields, tab to move, enter to continue"))
	}

	box := boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	help := helpStyle.Render("tab/↑/↓ move  ·  enter confirm  ·  esc quit")
	return lipgloss.JoinVertical(lipgloss.Center, "\n"+box, help+"\n")
}

// shelterStatusStyled renders shelter status with color matching its state.
func shelterStatusStyled(st shelter.State) string {
	switch st {
	case shelter.Connected:
		return okStyle.Render("connected")
	case shelter.Connecting:
		return valueStyle.Render("connecting...")
	case shelter.Failed:
		return failStyle.Render("failed")
	default:
		return failStyle.Render("disconnected")
	}
}

func (m Model) viewMain() string {
	var content string

	if m.loading && len(m.checks) == 0 {
		content = "running checks..."
	} else {
		lines := make([]string, 0, len(m.checks)+2)
		lines = append(lines, titleStyle.Width(46).Render("CONNECTIVITY"))
		lines = append(lines, "")
		for _, c := range m.checks {
			var status string
			if c.Label == "Public IP" {
				status = "    "
			} else if c.OK {
				status = okStyle.Render(" OK ")
			} else {
				status = failStyle.Render("FAIL")
			}
			lat := c.Latency
			if lat == "" {
				lat = "-"
			}
			line := checkLabelStyle.Render(c.Label) +
				checkTargetStyle.Render(c.Target) +
				status + "  " + valueStyle.Render(lat)
			lines = append(lines, line)
		}
		lines = append(lines, "")
		lines = append(lines, checkLabelStyle.Render("Shelter")+shelterStatusStyled(m.shelter.State))

		if m.loading {
			lines = append(lines, "")
			lines = append(lines, okStyle.Render("● running checks..."))
		} else if !m.lastRun.IsZero() {
			lines = append(lines, "")
			lines = append(lines, helpStyle.Render("updated "+m.lastRun.Format("15:04:05")))
		}
		content = lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	box := boxStyle.Render(content)
	help := helpStyle.Render("auto-refresh every 10s  ·  r refresh now  ·  c reconfigure  ·  q quit")

	return lipgloss.JoinVertical(lipgloss.Center, "\n"+box, help+"\n")
}
