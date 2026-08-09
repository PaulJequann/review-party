package reviewparty

type compiledProfile struct {
	revision ProfileRevision
	snapshot ProfileSnapshot
	reviewer reviewerRegistration
	prompt   string
	passes   []passPlan
}

type passPlan struct {
	name     string
	required bool
}

func compileProfile(catalog reviewerCatalog, name, reviewer string, subject ReviewSubject) (compiledProfile, error) {
	return (profileLibrary{}).compile(catalog, profileRequest{name: name, reviewer: reviewer}, subject)
}

func buildBugReviewPrompt(subject ReviewSubject) string {
	profile, err := (profileLibrary{}).resolve(defaultReviewerCatalog(), profileRequest{repository: subject.Repository, name: "bugs", reviewer: defaultReviewer})
	if err != nil {
		panic(err)
	}
	return renderReviewPrompt(profile, subject)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return "(none)"
	}
	result := lines[0]
	for _, line := range lines[1:] {
		result += "\n" + line
	}
	return result
}
