package configuration

import "fmt"

func (manager *Manager) Template(id string) (Template, bool) {
	for _, template := range manager.templates {
		if template.ID == id {
			return template, true
		}
	}
	return Template{}, false
}

func (manager *Manager) SkippedTemplates() []SkippedTemplate {
	return append([]SkippedTemplate{}, manager.skippedTemplates...)
}

func (manager *Manager) unknownTemplateError(id string) error {
	for _, skipped := range manager.skippedTemplates {
		if skipped.TemplateID == id {
			return fmt.Errorf("Review Profile Template %q was skipped: %s", id, skipped.Reason)
		}
	}
	return fmt.Errorf("unknown Review Profile Template %q", id)
}
