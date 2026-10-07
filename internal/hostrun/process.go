package hostrun

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sentinelArg = "--review-party-sentinel-v1"

const closeStopGrace = time.Second

const pipeDrainDelay = 2 * time.Second

var (
	errStopTimeout     = errors.New("process did not exit after forced termination")
	errSentinelTimeout = errors.New("the sentinel did not report in time")
)

var statusTimeout = 10 * time.Second

// Init turns this process into a sentinel when it was launched as one, and
// never returns in that case. Call it first in main, before flag parsing.
func Init() {
	if len(os.Args) < 2 || os.Args[1] != sentinelArg {
		return
	}
	os.Exit(runSentinel(os.Args[2:]))
}

// sentinelSpec travels over the control pipe so prompts never reach argv.
type sentinelSpec struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
	Env  []string `json:"env"`
}

type sentinelStatus struct {
	Reviewer Identity `json:"reviewer"`
	Group    int      `json:"group,omitempty"`
	Mode     TreeMode `json:"mode"`
	Error    string   `json:"error,omitempty"`
}

const (
	controlTerminate = 'T'
	controlKill      = 'K'
)

// Process is one Reviewer under a sentinel. The sentinel's exit status is
// the Reviewer's: its code, or 128 plus the signal number.
type Process struct {
	run        *Run
	sentinel   *exec.Cmd
	control    *os.File
	record     ProcessRecord
	recordPath string
	done       chan struct{}
	waitErr    error
}

// Start runs cmd's program under a sentinel inside the run. It honors Path,
// Args, Dir, Env, Stdin, Stdout, and Stderr; the sentinel owns process
// attributes and the process tree. A nil Env means the owner's environment.
// Either way the run's temp directory replaces TMPDIR, TMP, and TEMP.
func (r *Run) Start(cmd *exec.Cmd) (*Process, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	dir, err := r.ensureDir()
	if err != nil {
		return nil, err
	}
	spec := sentinelSpec{Path: cmd.Path, Args: cmd.Args, Dir: cmd.Dir, Env: withTemp(cmd.Env, filepath.Join(dir, tempName))}
	p, err := r.spawnSentinel(cmd, spec, filepath.Join(dir, procsName))
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	closed := r.closed
	if !closed {
		r.procs[p] = struct{}{}
	}
	r.mu.Unlock()
	if closed {
		_ = p.Stop(closeStopGrace) //nolint:errcheck // The caller gets errRunClosed either way.
		return nil, errRunClosed
	}
	go p.wait()
	return p, nil
}

func withTemp(env []string, dir string) []string {
	if env == nil {
		env = os.Environ()
	}
	kept := make([]string, 0, len(env)+len(tempVariables))
	for _, pair := range env {
		if !isTempVariable(pair) {
			kept = append(kept, pair)
		}
	}
	for _, name := range tempVariables {
		kept = append(kept, name+"="+dir)
	}
	return kept
}

func isTempVariable(pair string) bool {
	name, _, _ := strings.Cut(pair, "=")
	for _, candidate := range tempVariables {
		if strings.EqualFold(name, candidate) {
			return true
		}
	}
	return false
}

func (r *Run) spawnSentinel(cmd *exec.Cmd, spec sentinelSpec, recordDir string) (*Process, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		closeAll(controlRead, controlWrite)
		return nil, err
	}
	sentinel := exec.Command(executable, append([]string{sentinelArg}, sentinelArgs(controlRead, statusWrite)...)...)
	sentinel.Stdin, sentinel.Stdout, sentinel.Stderr = cmd.Stdin, cmd.Stdout, cmd.Stderr
	sentinel.WaitDelay = pipeDrainDelay
	attachPipes(sentinel, controlRead, statusWrite)
	err = sentinel.Start()
	closeAll(controlRead, statusWrite)
	if err != nil {
		closeAll(controlWrite, statusRead)
		return nil, err
	}
	status, err := exchange(controlWrite, statusRead, spec)
	closeAll(statusRead)
	if err != nil {
		closeAll(controlWrite)
		_ = sentinel.Process.Kill() //nolint:errcheck // A sentinel that already exited is the other way this path is reached.
		_ = sentinel.Wait()         //nolint:errcheck // The exchange error is the one worth returning.
		return nil, err
	}
	p := &Process{run: r, sentinel: sentinel, control: controlWrite, done: make(chan struct{})}
	p.recordPath = p.writeRecord(recordDir, spec, status)
	return p, nil
}

func exchange(control *os.File, statusPipe *os.File, spec sentinelSpec) (sentinelStatus, error) {
	if err := json.NewEncoder(control).Encode(spec); err != nil {
		return sentinelStatus{}, err
	}
	type reply struct {
		status sentinelStatus
		err    error
	}
	replies := make(chan reply, 1)
	go func() {
		status, err := readStatus(statusPipe)
		replies <- reply{status, err}
	}()
	timer := time.NewTimer(statusTimeout)
	defer timer.Stop()
	select {
	case r := <-replies:
		return r.status, r.err
	case <-timer.C:
		return sentinelStatus{}, errSentinelTimeout
	}
}

func readStatus(statusPipe *os.File) (sentinelStatus, error) {
	line, err := bufio.NewReader(statusPipe).ReadBytes('\n')
	if err != nil {
		return sentinelStatus{}, fmt.Errorf("the sentinel exited before reporting: %w", err)
	}
	var status sentinelStatus
	if err := json.Unmarshal(line, &status); err != nil {
		return sentinelStatus{}, err
	}
	if status.Error != "" {
		return sentinelStatus{}, errors.New(status.Error)
	}
	return status, nil
}

func (p *Process) writeRecord(dir string, spec sentinelSpec, status sentinelStatus) string {
	sentinel, err := identify(p.sentinel.Process.Pid)
	if err != nil {
		sentinel = Identity{PID: p.sentinel.Process.Pid, Boot: status.Reviewer.Boot}
	}
	p.record = ProcessRecord{
		Schema:   recordSchema,
		Sentinel: sentinel,
		Reviewer: status.Reviewer,
		Group:    status.Group,
		Started:  time.Now().UTC(),
		Program:  spec.Path,
		Mode:     status.Mode,
	}
	if status.Mode == TreeJobDegraded {
		p.run.mu.Lock()
		p.run.degraded = true
		p.run.mu.Unlock()
	}
	path := filepath.Join(dir, strconv.Itoa(sentinel.PID)+".json")
	err = os.MkdirAll(dir, 0o700)
	if err == nil {
		err = writeRecord(path, p.record)
	}
	if err != nil {
		p.run.warn(fmt.Sprintf("review-party could not record Reviewer process %d: %v; a crash will leave it to the user", status.Reviewer.PID, err))
		return ""
	}
	return path
}

func (p *Process) wait() {
	p.waitErr = p.sentinel.Wait()
	closeAll(p.control)
	killRecorded([]ProcessRecord{p.record})
	if p.recordPath != "" {
		_ = os.Remove(p.recordPath) //nolint:errcheck // A stale record names dead processes, which a reaper verifies before signalling.
	}
	p.run.mu.Lock()
	delete(p.run.procs, p)
	p.run.mu.Unlock()
	close(p.done)
}

// Done closes when the sentinel has exited and the record is gone.
func (p *Process) Done() <-chan struct{} { return p.done }

// Wait blocks until the Reviewer is finished and returns the sentinel's exit
// error, an *exec.ExitError carrying the Reviewer's status. A Reviewer that
// died by signal reports 128 plus the signal number.
func (p *Process) Wait() error {
	<-p.done
	return p.waitErr
}

// Stop asks the sentinel to terminate the tree, then to kill it, then kills
// the sentinel itself, allowing grace for each step. It returns nil once
// Done is closed and errStopTimeout when even the sentinel will not die.
func (p *Process) Stop(grace time.Duration) error {
	phases := []func(){
		func() { _, _ = p.control.Write([]byte{controlTerminate}) }, //nolint:errcheck // A write fails only once the sentinel is gone, and then Done closes.
		func() { _, _ = p.control.Write([]byte{controlKill}) },      //nolint:errcheck // Same.
		func() { _ = p.sentinel.Process.Kill() },                    //nolint:errcheck // Same.
	}
	for _, phase := range phases {
		select {
		case <-p.done:
			return nil
		default:
		}
		phase()
		select {
		case <-p.done:
			return nil
		case <-time.After(grace):
		}
	}
	return errStopTimeout
}

func closeAll(files ...*os.File) {
	for _, file := range files {
		_ = file.Close() //nolint:errcheck // Pipe ends and lock files carry nothing a late close could lose.
	}
}
