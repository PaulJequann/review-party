package configuration

func decodeDomainObject(payload []byte, objectName string, destination any, fields ...string) error {
	if _, err := decodeObjectFields(payload, objectName, fields...); err != nil {
		return err
	}
	return strictDecode(payload, destination)
}

func (reference *ProfileReference) UnmarshalJSON(payload []byte) error {
	type plain ProfileReference
	return decodeDomainObject(payload, "profile reference", (*plain)(reference), "scope", "profile")
}

func (item *SelectionItem) UnmarshalJSON(payload []byte) error {
	type plain SelectionItem
	if err := decodeDomainObject(payload, "review selection item", (*plain)(item), "profile", "party"); err != nil {
		return err
	}
	_, err := item.Name()
	return err
}

func (selection *ReviewSelection) UnmarshalJSON(payload []byte) error {
	type plain ReviewSelection
	return decodeDomainObject(payload, "reviews", (*plain)(selection), "concurrency_limit", "global", "repository")
}

func (profile *Profile) UnmarshalJSON(payload []byte) error {
	type plain Profile
	return decodeDomainObject(payload, "profile", (*plain)(profile), "schema_version", "name", "reviewer", "model", "reasoning_effort", "attempt_deadline", "template_id", "template_revision")
}

func (party *Party) UnmarshalJSON(payload []byte) error {
	type plain Party
	return decodeDomainObject(payload, "party", (*plain)(party), "schema_version", "name", "description", "concurrency_limit", "profiles")
}
