package httpapi

import "example.com/preferences/internal/settings"

func ApplyEmailPreference(current settings.Settings, enabled bool) settings.Settings {
	current.EmailNotifications = enabled
	return current
}
