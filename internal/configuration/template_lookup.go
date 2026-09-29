package configuration

func (manager *Manager) Template(id string) (Template, bool) {
	for _, template := range manager.templates {
		if template.ID == id {
			return template, true
		}
	}
	return Template{}, false
}
