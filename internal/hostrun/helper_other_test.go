//go:build unix && !linux

package hostrun

import "os/exec"

func escapeeCommand(string) *exec.Cmd { return nil }
