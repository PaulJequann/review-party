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
