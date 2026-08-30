package configurationhub

import "charm.land/lipgloss/v2"

// The Hub uses terminal-native ANSI roles so its colors follow the user's
// terminal theme. Lip Gloss LightDark/Complete adaptive hex colors, gradient
// borders, and the no-mistakes v1 textinput wizard are deliberately not
// adopted here; only the shared visual vocabulary is carried over.
const (
	roleRed         = "1"
	roleGreen       = "2"
	roleYellow      = "3"
	roleBlue        = "4"
	roleCyan        = "6"
	roleBrightBlack = "8"
)

var (
	sectionTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(roleCyan))
	metaStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color(roleBrightBlack))
	focusStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color(roleBlue))
	warningStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(roleYellow))
	dangerStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(roleRed))
	successStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(roleGreen))
	borderStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(roleBrightBlack))
)

func globalScopeStyle() lipgloss.Style {
	return sectionTitleStyle.UnsetBold().Faint(true)
}

func repositoryScopeStyle() lipgloss.Style {
	return focusStyle
}

func renderActionKey(value string) string {
	return sectionTitleStyle.UnsetForeground().Render(value)
}

// stripANSI keeps Render suitable for accessible mode even when a child view
// has already rendered styled content.
func stripANSI(value string) string {
	var plain []byte
	for index := 0; index < len(value); index++ {
		if value[index] != '\x1b' {
			plain = append(plain, value[index])
			continue
		}
		index = skipANSIEscape(value, index)
	}
	return string(plain)
}

func skipANSIEscape(value string, index int) int {
	if index+1 >= len(value) {
		return index
	}
	switch value[index+1] {
	case '[':
		return skipCSISequence(value, index+1)
	case ']':
		return skipOSCSequence(value, index+1)
	default:
		return index + 1
	}
}

func skipCSISequence(value string, index int) int {
	for index++; index < len(value); index++ {
		if value[index] >= 0x40 && value[index] <= 0x7e {
			return index
		}
	}
	return len(value) - 1
}

func skipOSCSequence(value string, index int) int {
	for index++; index < len(value); index++ {
		if value[index] == '\a' {
			return index
		}
		if isOSCStringTerminator(value, index) {
			return index + 1
		}
	}
	return len(value) - 1
}

func isOSCStringTerminator(value string, index int) bool {
	return value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '\\'
}
