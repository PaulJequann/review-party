package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const skillTemplatePrefix = "skill:"

const maximumListedBundledFiles = 5

func SkillTemplates(rootsByPrecedence []string) []Template {
	seen := map[string]bool{}
	var templates []Template
	for _, root := range rootsByPrecedence {
		templates = append(templates, rootSkillTemplates(root, seen)...)
	}
	return templates
}

func rootSkillTemplates(root string, seen map[string]bool) []Template {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var templates []Template
	for _, entry := range entries {
		name := entry.Name()
		if seen[name] || strings.HasPrefix(name, ".") {
			continue
		}
		if template, ok := loadSkillTemplate(filepath.Join(root, name), name); ok {
			seen[name] = true
			templates = append(templates, template)
		}
	}
	return templates
}

func loadSkillTemplate(directory, name string) (Template, bool) {
	content, found, err := readRegularFile(directory, filepath.Join(directory, "SKILL.md"), "skill", MaximumDocumentBytes)
	if err != nil || !found {
		return Template{}, false
	}
	body := stripFrontmatter(string(content))
	if body == "" {
		return Template{}, false
	}
	instructions := skillInstructions(name, body)
	digest := sha256.Sum256([]byte(instructions))
	return Template{
		ID:           skillTemplatePrefix + name,
		Revision:     "sha256-" + hex.EncodeToString(digest[:])[:12],
		Instructions: instructions,
		BundledFiles: skillBundledFiles(directory),
	}, true
}

func stripFrontmatter(content string) string {
	lines := strings.SplitAfter(content, "\n")
	if strings.TrimRight(lines[0], "\r\n") != "---" {
		return strings.TrimSpace(content)
	}
	for index := 1; index < len(lines); index++ {
		if strings.TrimRight(lines[index], "\r\n") == "---" {
			return strings.TrimSpace(strings.Join(lines[index+1:], ""))
		}
	}
	return strings.TrimSpace(content)
}

func skillInstructions(name, body string) string {
	return "This Profile was imported from the `" + name + "` skill. Review Party runs it as a read-only Reviewer.\n\n" +
		"- You can read and search this repository only. Skip skill steps that need shell commands, edits, other skills, subagents, or files bundled with the skill.\n" +
		"- When the skill requires observed evidence you could not gather, such as a test failing after a mutation, state that the claim is unverified.\n" +
		"- Report only through the required result block, not the skill's own output format, and do not apply changes.\n\n" +
		"---\n\n" + body + "\n"
}

func skillBundledFiles(directory string) []string {
	skill := os.DirFS(directory)
	var files []string
	err := fs.WalkDir(skill, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return nil
		}
		if !bundledGuidance(path, entry) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if info, statErr := fs.Stat(skill, path); statErr == nil && info.Mode().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil
	}
	sort.Strings(files)
	return files
}

// agents/ holds Codex UI metadata (agents/openai.yaml), not skill guidance.
func bundledGuidance(path string, entry fs.DirEntry) bool {
	return path != "SKILL.md" && path != "agents" && !strings.HasPrefix(entry.Name(), ".")
}

func (template Template) bundledFilesWarning() string {
	count := len(template.BundledFiles)
	if count == 0 {
		return ""
	}
	noun := "files"
	if count == 1 {
		noun = "file"
	}
	listed := template.BundledFiles[:min(count, maximumListedBundledFiles)]
	warning := fmt.Sprintf("Template %s bundles %d %s the Reviewer cannot read: %s", template.ID, count, noun, strings.Join(listed, ", "))
	if count > len(listed) {
		warning += fmt.Sprintf(", and %d more", count-len(listed))
	}
	return warning
}
