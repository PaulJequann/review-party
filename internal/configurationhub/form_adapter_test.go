package configurationhub

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

func TestFormExternalEditorReturnsText(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("shell unavailable: %v", err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	sentinel := filepath.Join(t.TempDir(), "launched")
	model := New(Snapshot{})
	model.session.instructions = "draft instructions"
	model.openForm(formProfileInstructions, []huh.Field{
		huh.NewText().Value(&model.session.instructions).
			Editor("sh", "-c", `printf 'edited instructions' > "$2"; printf launched > "$1"`, "editor", sentinel),
	})
	_, command := model.form.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	model = requireHubModel(t, updated)
	if model.quit == nil {
		t.Fatal("draft did not open quit confirmation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	program := tea.NewProgram(editorFormModel{Model: model, command: command}, tea.WithContext(ctx),
		tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer())
	final, err := program.Run()
	if _, sentinelErr := os.Stat(sentinel); sentinelErr != nil {
		t.Fatalf("Ctrl+E did not launch the editor: %v (program: %v)", sentinelErr, err)
	}
	if err != nil {
		t.Fatalf("editor did not return text to the originating form: %v", err)
	}
	if model.session.instructions != "edited instructions" {
		t.Fatalf("instructions = %q", model.session.instructions)
	}
	if finished, ok := final.(editorFormModel); !ok || finished.quit == nil || finished.drafts.profile.Instructions != "edited instructions" {
		t.Fatal("editor result did not update the draft behind the quit overlay")
	}
}

type editorFormModel struct {
	Model
	command tea.Cmd
}

func (model editorFormModel) Init() tea.Cmd { return model.command }

func (model editorFormModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	updated, command := model.Model.Update(message)
	if hub, ok := updated.(Model); ok {
		model.Model = hub
	}
	if model.session.instructions == "edited instructions" {
		return model, tea.Quit
	}
	return model, command
}
