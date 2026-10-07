package main

import (
	"strings"
	"testing"
)

func TestAppendJSONArrayElementKeepsEveryExistingByte(t *testing.T) {
	element := map[string]string{"matcher": "Bash", "command": "a >/dev/null 2>&1 && b"}
	cases := []struct{ name, content, want string }{
		{
			name: "existing array",
			content: `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Edit", "hooks": []}
    ]
  },
  "z": 1
}
`,
			want: `{
  "model": "opus",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Edit", "hooks": []},
      {
        "command": "a >/dev/null 2>&1 && b",
        "matcher": "Bash"
      }
    ]
  },
  "z": 1
}
`,
		},
		{
			name:    "missing keys in a tab-indented file",
			content: "{\n\t\"model\": \"opus\"\n}",
			want:    "{\n\t\"model\": \"opus\",\n\t\"hooks\": {\n\t\t\"PreToolUse\": [\n\t\t\t{\n\t\t\t\t\"command\": \"a >/dev/null 2>&1 && b\",\n\t\t\t\t\"matcher\": \"Bash\"\n\t\t\t}\n\t\t]\n\t}\n}",
		},
		{
			name:    "compact",
			content: `{"model":"opus","hooks":{"Stop":[]}}` + "\n",
			want:    `{"model":"opus","hooks":{"Stop":[],"PreToolUse":[{"command":"a >/dev/null 2>&1 && b","matcher":"Bash"}]}}` + "\n",
		},
		{
			name:    "empty array",
			content: "{\n    \"hooks\": {\n        \"PreToolUse\": []\n    }\n}\n",
			want:    "{\n    \"hooks\": {\n        \"PreToolUse\": [\n            {\n                \"command\": \"a >/dev/null 2>&1 && b\",\n                \"matcher\": \"Bash\"\n            }\n        ]\n    }\n}\n",
		},
		{
			name:    "empty object",
			content: "{}\n",
			want:    "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"command\": \"a >/dev/null 2>&1 && b\",\n        \"matcher\": \"Bash\"\n      }\n    ]\n  }\n}\n",
		},
		{
			name:    "no file",
			content: "",
			want:    "{\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\n        \"command\": \"a >/dev/null 2>&1 && b\",\n        \"matcher\": \"Bash\"\n      }\n    ]\n  }\n}\n",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := appendJSONArrayElement([]byte(test.content), []string{"hooks", "PreToolUse"}, element)
			if err != nil || string(got) != test.want {
				t.Fatalf("appendJSONArrayElement = %v\n%s\nwant\n%s", err, got, test.want)
			}
		})
	}
}

func TestAppendJSONArrayElementRejectsAnUnexpectedShape(t *testing.T) {
	for content, want := range map[string]string{
		`[]`:                            "not a JSON object",
		`{"hooks": []}`:                 "hooks: want a JSON object",
		`{"hooks": {"PreToolUse": {}}}`: "hooks.PreToolUse: want a JSON array",
		`{"hooks": `:                    "unexpected EOF",
	} {
		if _, err := appendJSONArrayElement([]byte(content), []string{"hooks", "PreToolUse"}, 1); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("appendJSONArrayElement(%s) error = %v, want %q", content, err, want)
		}
	}
}

func TestRemoveJSONArrayElementRestoresTheDocumentBeforeTheAppend(t *testing.T) {
	element := map[string]string{"matcher": "Bash", "command": "a >/dev/null 2>&1 && b"}
	for _, content := range []string{
		"{\n  \"model\": \"opus\",\n  \"hooks\": {\n    \"PreToolUse\": [\n      {\"matcher\": \"Edit\", \"hooks\": []}\n    ]\n  },\n  \"z\": 1\n}\n",
		"{\n\t\"model\": \"opus\"\n}",
		`{"model":"opus","hooks":{"Stop":[]}}` + "\n",
		"{\n  \"hooks\": {\n    \"Stop\": []\n  }\n}\n",
		"{}\n",
		"{}",
	} {
		appended, err := appendJSONArrayElement([]byte(content), jsonPath{"hooks", "PreToolUse"}, element)
		if err != nil {
			t.Fatal(err)
		}
		if got, removed := removeHooks(t, string(appended), element); got != content || !removed {
			t.Errorf("remove after append = %v\n%s\nwant\n%s", removed, got, content)
		}
	}
}

func TestRemoveJSONArrayElementTakesEveryCopyWhereverItSits(t *testing.T) {
	ours := `{"matcher":"Bash","hooks":[{"type":"command","command":"x"}]}`
	element := agentHookGroup{Matcher: "Bash", Hooks: []agentHookHandler{{Type: "command", Command: "x"}}}
	cases := []struct{ name, content, want string }{
		{"first of several", "{\"hooks\": {\"PreToolUse\": [\n  " + ours + ",\n  {\"matcher\": \"Edit\"}\n]}}\n", "{\"hooks\": {\"PreToolUse\": [\n  {\"matcher\": \"Edit\"}\n]}}\n"},
		{"between others", `{"hooks":{"PreToolUse":[1,` + ours + `,2]}}`, `{"hooks":{"PreToolUse":[1,2]}}`},
		{"twice", `{"a":1,"hooks":{"PreToolUse":[` + ours + `,` + ours + `]}}`, `{"a":1}`},
		{"hooks before other keys", `{"hooks":{"PreToolUse":[` + ours + `]},"a":1}`, `{"a":1}`},
		{"key order and spacing differ", `{"hooks":{"PreToolUse":[{ "hooks": [{"command":"x","type":"command"}], "matcher":"Bash" }]},"a":1}`, `{"a":1}`},
	}
	for _, test := range cases {
		if got, removed := removeHooks(t, test.content, element); got != test.want || !removed {
			t.Errorf("%s: remove = %v\n%s\nwant\n%s", test.name, removed, got, test.want)
		}
	}
}

func TestRemoveJSONArrayElementLeavesOtherShapesAlone(t *testing.T) {
	element := map[string]string{"matcher": "Bash"}
	for _, content := range []string{
		"",
		"  \n",
		`{"hooks": []}`,
		`{"hooks": {"PreToolUse": {"matcher": "Bash"}}}`,
		`{"hooks": {"PreToolUse": [{"matcher": "Bash", "extra": true}]}}`,
		`{"other": {"PreToolUse": [{"matcher": "Bash"}]}}`,
	} {
		if got, removed := removeHooks(t, content, element); got != content || removed {
			t.Errorf("remove(%s) = %v, %s; want it unchanged", content, removed, got)
		}
	}
	if _, _, err := removeJSONArrayElement([]byte(`{"hooks": `), jsonPath{"hooks", "PreToolUse"}, element); err == nil {
		t.Error("remove from truncated JSON succeeded")
	}
}

func removeHooks(t *testing.T, content string, element any) (string, bool) {
	t.Helper()
	got, removed, err := removeJSONArrayElement([]byte(content), jsonPath{"hooks", "PreToolUse"}, element)
	if err != nil {
		t.Fatalf("remove(%s): %v", content, err)
	}
	return string(got), removed
}
