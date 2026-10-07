//go:build darwin

package hostrun

import (
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

// SZOMB from sys/proc.h; x/sys does not export the p_stat values.
const procStateZombie = 5

var bootID = sync.OnceValues(func() (string, error) {
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(boot.Sec, 10) + "." + strconv.FormatInt(int64(boot.Usec), 10), nil
})

func identify(pid int) (Identity, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return Identity{}, err
	}
	if int(proc.Proc.P_pid) != pid || proc.Proc.P_stat == procStateZombie {
		return Identity{}, errProcessGone
	}
	boot, err := bootID()
	if err != nil {
		return Identity{}, err
	}
	start := uint64(proc.Proc.P_starttime.Sec)*1_000_000 + uint64(proc.Proc.P_starttime.Usec)
	return Identity{PID: pid, Start: start, Boot: boot}, nil
}
