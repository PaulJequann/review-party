package hostrun

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
)

const (
	sentinelSetupFailed = 2
)

type tree struct {
	pid   int
	group int
	mode  TreeMode
	platformTree
}

func runSentinel(args []string) int {
	control, status, err := sentinelPipes(args)
	if err != nil {
		return sentinelSetupFailed
	}
	reader := bufio.NewReader(control)
	t, cmd, err := startReviewer(reader)
	if err != nil {
		report(status, sentinelStatus{Error: err.Error()})
		return sentinelSetupFailed
	}
	reviewer, err := identify(t.pid)
	if err != nil {
		reviewer = Identity{PID: t.pid}
	}
	report(status, sentinelStatus{Reviewer: reviewer, Group: t.group, Mode: t.mode})
	releaseOwnerPipes()
	go t.listen(reader)
	_ = cmd.Wait() //nolint:errcheck // exitStatus reads the outcome from ProcessState.
	t.sweep()
	return exitStatus(cmd.ProcessState)
}

func startReviewer(reader *bufio.Reader) (*tree, *exec.Cmd, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, nil, err
	}
	var spec sentinelSpec
	if err := json.Unmarshal(line, &spec); err != nil {
		return nil, nil, err
	}
	platform, mode, err := newPlatformTree()
	if err != nil {
		return nil, nil, err
	}
	t := &tree{mode: mode, platformTree: platform}
	cmd := exec.Command(spec.Path)
	cmd.Args = spec.Args
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	t.attach(cmd)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	t.started(cmd.Process.Pid)
	return t, cmd, nil
}

func report(status *os.File, s sentinelStatus) {
	_ = json.NewEncoder(status).Encode(s) //nolint:errcheck // The owner treats a missing line as a failed start.
	_ = status.Close()                    //nolint:errcheck // The pipe's only job was that one line.
}

func (t *tree) listen(reader *bufio.Reader) {
	for {
		b, err := reader.ReadByte()
		if err != nil {
			t.kill()
			return
		}
		switch b {
		case controlTerminate:
			t.terminate()
		case controlKill:
			t.kill()
		}
	}
}
