package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// describeAgentCommands renders what the hook found in a shell command, one
// git command per line.
func describeAgentCommands(commands []agentGitCommand) string {
	forms := map[agentCommandForm]string{formPush: "push", formCommitStaged: "staged", formCommitTracked: "tracked", formCommitPaths: "paths", formUndecidable: "undecidable"}
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		line := fmt.Sprintf("%s %s", command.checkpoint, forms[command.form])
		if command.directory != "" {
			line += " in " + command.directory
		}
		if command.preceded {
			line += " preceded"
		}
		if command.form == formPush {
			line += fmt.Sprintf(" to %q %q", command.push.Remote, command.push.Refspecs)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func TestFindAgentGitCommands(t *testing.T) {
	cases := []struct{ command, want string }{
		{"ls -la", ""},
		{"git status && git log --grep commit", ""},
		{`echo "git push" # git commit`, ""},
		{"git push --dry-run origin main", ""},
		{"git commit --dry-run", ""},
		{"git push", `pre-push push to "" []`},
		{"/usr/bin/git push origin HEAD:main +feature", `pre-push push to "origin" ["HEAD:main" "+feature"]`},
		{"git push -u origin feature 2>&1 | tail -5", `pre-push push to "origin" ["feature"]`},
		{"git push -fo ci.skip --repo=fork", `pre-push push to "fork" []`},
		{"git -C ../other --no-pager -c core.pager=cat push", `pre-push push in ../other to "" []`},
		{"git -c push.default=matching push", "pre-push undecidable"},
		{"git -c Remote.origin.push=refs/heads/*:refs/heads/* push", "pre-push undecidable"},
		{"git push --dry-run --no-dry-run origin main", `pre-push push to "origin" ["main"]`},
		{"git push --no-dry-run -n origin main", ""},
		{"git commit --dry-run --no-dry-run -m x", "pre-commit staged"},
		{"GIT_DIR=elsewhere/.git git push", "pre-push undecidable"},
		{"git push --mir origin", "pre-push undecidable"},
		{"git push --dry origin main", "pre-push undecidable"},
		{"git push --force-with-lease --no-verify origin main", `pre-push push to "origin" ["main"]`},
		{"git commit --pat", "pre-commit undecidable"},
		{"git commit --allow-empty -m x", "pre-commit staged"},
		{"git --config-env=remote.origin.push=SPEC push origin", "pre-push undecidable"},
		{"env -u UNUSED git push", "pre-push undecidable"},
		{"env -u UNUSED ls", ""},
		{"env GIT_INDEX_FILE=/tmp/index git commit -m x", "pre-commit undecidable"},
		{"git push --all origin", "pre-push undecidable"},
		{"git push origin --delete old", "pre-push undecidable"},
		{"git --git-dir=elsewhere/.git push", "pre-push undecidable"},
		{"git --frobnicate commit -m x", "pre-commit undecidable"},
		{"git -C /work -C app commit -m 'push it'", "pre-commit staged in /work/app"},
		{"GIT_TRACE=1 git commit -c HEAD", "pre-commit staged"},
		{`git commit -am "fix: the thing"`, "pre-commit tracked"},
		{"git commit --all --message=x --no-verify", "pre-commit tracked"},
		{"git commit -p", "pre-commit paths"},
		{"git commit --only -m x", "pre-commit paths"},
		{"git commit -i -m x", "pre-commit paths"},
		{"git commit -m x a.go", "pre-commit paths"},
		{"git commit -m x -- a.go", "pre-commit paths"},
		{"git add -A && git commit -m x", "pre-commit staged preceded"},
		{"git commit -m x && git push", "pre-commit staged\npre-push push preceded to \"\" []"},
		{"cd app; git push", `pre-push push in app to "" []`},
		{"cd /work && cd app && git -C sub commit -m x && git push", "pre-commit staged in /work/app/sub\npre-push push in /work/app preceded to \"\" []"},
		{"(cd app && git push)", `pre-push push preceded to "" []`},
		{"cd app && (git push)", `pre-push push in app to "" []`},
		{"cd app || git push", `pre-push push preceded to "" []`},
		{"cd app | git push", `pre-push push preceded to "" []`},
		{"echo hi && cd app && git push", `pre-push push in app preceded to "" []`},
		{"true || cd app && git push", `pre-push push preceded to "" []`},
		{"cd $X && git push", `pre-push push preceded to "" []`},
		{"cd ~/app && git push", `pre-push push preceded to "" []`},
		{"cd -P app && git push", `pre-push push preceded to "" []`},
		{"(git push)", `pre-push push to "" []`},
		{"git commit -m \"$(cat <<'EOF'\nfix: the thing\n\nThen git push (later); don't \"wait\"\nEOF\n)\"", "pre-commit staged"},
		{"git commit -F - <<EOF\nmessage && git push\nEOF\n", "pre-commit staged"},
		{"git commit -m `date` && echo done", "pre-commit staged"},
	}
	for _, testCase := range cases {
		commands, err := findAgentGitCommands(testCase.command)
		if got := describeAgentCommands(commands); err != nil || got != testCase.want {
			t.Errorf("findAgentGitCommands(%q) = %q, %v\nwant %q", testCase.command, got, err, testCase.want)
		}
	}
}

func TestFindAgentGitCommandsRejectsAnUnterminatedQuote(t *testing.T) {
	for _, command := range []string{`git commit -m "unfinished`, "git commit -m 'unfinished", "git commit -m \"$(cat <<EOF\nmsg\n\"", "git push `"} {
		if commands, err := findAgentGitCommands(command); err == nil {
			t.Errorf("findAgentGitCommands(%q) = %q, want an error", command, describeAgentCommands(commands))
		}
	}
}

func TestSplitShellCommandDropsRedirectionsAndKeepsQuotedWords(t *testing.T) {
	segments, err := splitShellCommand(`a 'b c' "d\"e" f\ g 2>/dev/null >>out <in 3<&0 &>all h # i j` + "\nk|&l")
	want := []shellSegment{{words: shellWords{"a", "b c", `d"e`, "f g", "h"}, operator: "\n"}, {words: shellWords{"k"}, operator: "|&"}, {words: shellWords{"l"}}}
	if err != nil || !slices.EqualFunc(segments, want, sameSegment) {
		t.Fatalf("segments = %+v, %v\nwant %+v", segments, err, want)
	}
}

func sameSegment(a, b shellSegment) bool {
	return slices.Equal(a.words, b.words) && a.operator == b.operator && a.depth == b.depth
}

func TestSplitShellCommandKeepsEachCommandsOperatorAndDepth(t *testing.T) {
	segments, err := splitShellCommand("a && (b || c) | d; e &")
	want := []shellSegment{
		{words: shellWords{"a"}, operator: "&&"}, {words: shellWords{"b"}, operator: "||", depth: 1},
		{words: shellWords{"c"}, operator: ")", depth: 1}, {words: shellWords{"d"}, operator: ";"}, {words: shellWords{"e"}, operator: "&"},
	}
	if err != nil || !slices.EqualFunc(segments, want, sameSegment) {
		t.Fatalf("segments = %+v, %v\nwant %+v", segments, err, want)
	}
}
