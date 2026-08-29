//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package configurationhub

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"unsafe"
)

func configureEditorProcessGroup(command *exec.Cmd) (func() error, error) {
	attributes := &syscall.SysProcAttr{Setpgid: true}
	restoreTerminal := func() error { return nil }
	terminal, ok := command.Stdin.(*os.File)
	if ok {
		foreground, err := editorTerminalForegroundProcessGroup(terminal)
		if err != nil && !errors.Is(err, syscall.ENOTTY) {
			return nil, fmt.Errorf("inspect terminal foreground process group: %w", err)
		}
		if err == nil {
			attributes.Foreground = true
			attributes.Ctty = int(terminal.Fd())
			restoreTerminal = func() error {
				return setEditorTerminalForegroundProcessGroup(terminal, foreground)
			}
		}
	}
	command.SysProcAttr = attributes
	return restoreTerminal, nil
}

func terminateEditorProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGTERM) //nolint:errcheck // Process-group signaling is best-effort.
	}
}

func killEditorProcessGroup(command *exec.Cmd) {
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL) //nolint:errcheck // Process-group signaling is best-effort.
	}
}

func editorTerminalForegroundProcessGroup(terminal *os.File) (int32, error) {
	var foreground int32
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, terminal.Fd(), uintptr(syscall.TIOCGPGRP), uintptr(unsafe.Pointer(&foreground)), 0, 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return foreground, nil
}

func setEditorTerminalForegroundProcessGroup(terminal *os.File, foreground int32) error {
	wasIgnored := signal.Ignored(syscall.SIGTTOU)
	signal.Ignore(syscall.SIGTTOU)
	defer func() {
		if !wasIgnored {
			signal.Reset(syscall.SIGTTOU)
		}
	}()
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, terminal.Fd(), uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&foreground)), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
