package configurationhub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

const editorProcessCleanupGrace = time.Second

func editInstructions(ctx context.Context, instructions string, input io.Reader, output io.Writer) (string, error) {
	editorCommand, err := parseCommandLine(os.Getenv("EDITOR"))
	if err != nil {
		return "", fmt.Errorf("parse $EDITOR: %w", err)
	}
	if len(editorCommand) == 0 {
		return "", fmt.Errorf("$EDITOR is not configured")
	}
	path, err := writeInstructionFile(instructions)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(path) }() //nolint:errcheck // Temporary editor cleanup cannot change the editing result.
	if ctx == nil {
		ctx = context.Background()
	}
	command := exec.Command(editorCommand[0], append(editorCommand[1:], path)...)
	command.Stdin, command.Stdout, command.Stderr = input, output, output
	if err := runEditorCommand(ctx, command); err != nil {
		return "", fmt.Errorf("run $EDITOR: %w", err)
	}
	payload, err := os.ReadFile(path)
	return string(payload), err
}

func runEditorCommand(ctx context.Context, command *exec.Cmd) error {
	restoreTerminal, err := configureEditorProcessGroup(command)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return errors.Join(err, restoreTerminal())
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	var commandErr error
	select {
	case commandErr = <-finished:
	case <-ctx.Done():
		commandErr = ctx.Err()
		if err := stopEditorProcess(command, finished); err != nil {
			commandErr = errors.Join(commandErr, err)
		}
	}
	if err := restoreTerminal(); err != nil {
		commandErr = errors.Join(commandErr, fmt.Errorf("restore terminal foreground process group: %w", err))
	}
	return commandErr
}

func stopEditorProcess(command *exec.Cmd, finished <-chan error) error {
	terminateEditorProcessGroup(command)
	if editorProcessStopped(finished, editorProcessCleanupGrace) {
		return nil
	}
	killEditorProcessGroup(command)
	if editorProcessStopped(finished, editorProcessCleanupGrace) {
		return nil
	}
	return errors.New("editor process did not terminate after forced cleanup")
}

func editorProcessStopped(finished <-chan error, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-finished:
		return true
	case <-timer.C:
		return false
	}
}

func writeInstructionFile(instructions string) (string, error) {
	file, err := os.CreateTemp("", "review-party-instructions-*.md")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if _, err := file.WriteString(instructions); err != nil {
		closeErr := file.Close()
		return "", cleanupInstructionFile(path, errors.Join(fmt.Errorf("write instructions: %w", err), closeErr))
	}
	if err := file.Close(); err != nil {
		return "", cleanupInstructionFile(path, fmt.Errorf("close instructions: %w", err))
	}
	return path, nil
}

func cleanupInstructionFile(path string, cause error) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(cause, fmt.Errorf("remove temporary instructions: %w", err))
	}
	return cause
}
