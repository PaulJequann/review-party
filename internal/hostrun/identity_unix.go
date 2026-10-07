//go:build unix

package hostrun

import "syscall"

func sameBoot(a, b string) bool { return a == b }

func killProcess(id Identity) {
	if !id.alive() {
		return
	}
	_ = syscall.Kill(id.PID, syscall.SIGKILL) //nolint:errcheck // A process that is already gone is the goal.
}

// Linux and macOS never reuse a group id while a member lives, so a missing
// leader means survivors or an empty group, and a stranger holding the
// leader's PID means the group is not ours any more.
func killGroup(group int, leader Identity) {
	current, err := identify(group)
	if err == nil && current.Start != leader.Start {
		return
	}
	_ = syscall.Kill(-group, syscall.SIGKILL) //nolint:errcheck // A process that is already gone is the goal.
}
