package configurationhub

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"reviewparty/internal/configuration"
)

// The two Hub runtimes must walk the profile flow in the same stage order.
// Regression: the accessible loop asked for the instruction source before the
// profile fields, while the interactive forms asked afterwards, so the two
// interfaces disagreed about one domain flow.
func TestAccessibleProfileFlowAsksForInstructionsAfterFields(t *testing.T) {
	repository := configuration.Repository(t.TempDir())
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	// Bounded context: if the flow's stage order does not match this script, the
	// prompts misread the script and the run must abort instead of hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := strings.Join([]string{
		"2",            // scope: Repository
		"flow-probe",   // fields: name
		"codex",        // fields: reviewer
		"luna",         // fields: model
		"high",         // fields: effort
		"8m",           // fields: deadline
		"2",            // instruction source: Blank (asked after fields)
		"Review bugs.", // instructions text
		"n",            // keep as typed (no $EDITOR)
		"y",            // publish the plan
	}, "\n")
	var output bytes.Buffer
	editor := &editor{
		manager: manager,
		RunOptions: RunOptions{
			Context: ctx, Repository: repository, Accessible: true,
			Input: newHoldingInput(input), Output: &output,
		},
	}
	if err := editor.createProfile(); err != nil {
		t.Fatalf("createProfile: %v\noutput:\n%s", err, output.String())
	}

	fieldsAt := strings.Index(output.String(), "Profile name")
	sourceAt := strings.Index(output.String(), "Instruction source")
	if fieldsAt < 0 || sourceAt < 0 {
		t.Fatalf("missing prompts in output: fields %d source %d", fieldsAt, sourceAt)
	}
	if fieldsAt > sourceAt {
		t.Fatalf("fields prompted after instruction source: fields %d, source %d", fieldsAt, sourceAt)
	}
	if _, found, err := manager.LoadProfile(configuration.ScopeRepository, repository, "flow-probe"); err != nil || !found {
		t.Fatalf("published profile found=%v err=%v", found, err)
	}
}

// holdingInput serves one script line per Read (huh accessible prompts create
// a fresh bufio.Scanner per field, so buffered-overhead would be lost), then
// blocks instead of returning EOF, so a stage-order mismatch aborts through
// the context with output intact rather than panicking inside huh.
type holdingInput struct {
	lines   []string
	next    int
	release chan struct{}
	once    sync.Once
}

func newHoldingInput(script string) *holdingInput {
	return &holdingInput{lines: strings.Split(script, "\n"), release: make(chan struct{})}
}

func (input *holdingInput) Read(p []byte) (int, error) {
	if input.next >= len(input.lines) {
		<-input.release
		return 0, io.EOF
	}
	line := input.lines[input.next] + "\n"
	input.next++
	if len(line) > len(p) {
		line = line[:len(p)]
	}
	return copy(p, line), nil
}

func (input *holdingInput) Close() error {
	input.once.Do(func() { close(input.release) })
	return nil
}
