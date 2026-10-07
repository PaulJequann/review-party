//go:build windows

package hostrun

import (
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// STILL_ACTIVE from winbase.h; x/sys does not export it.
const stillActive = 259

// A boot stamp derived from the tick count drifts by a second or two between
// reads, so two stamps this close apart name the same boot.
const bootStampTolerance = 5

var procGetTickCount64 = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64")

var bootID = sync.OnceValues(func() (string, error) {
	if err := procGetTickCount64.Find(); err != nil {
		return "", err
	}
	ticks, _, _ := procGetTickCount64.Call() //nolint:errcheck // GetTickCount64 cannot fail; the third value is a stale last-error.
	booted := time.Now().Unix() - int64(uint64(ticks)/1000)
	return strconv.FormatInt(booted, 10), nil
})

func sameBoot(a, b string) bool {
	first, err := strconv.ParseInt(a, 10, 64)
	if err != nil {
		return false
	}
	second, err := strconv.ParseInt(b, 10, 64)
	if err != nil {
		return false
	}
	return max(first, second)-min(first, second) <= bootStampTolerance
}

func identify(pid int) (Identity, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return Identity{}, err
	}
	defer windows.CloseHandle(handle) //nolint:errcheck // Closing a query handle cannot fail in a way the caller can act on.
	return identifyHandle(pid, handle)
}

func identifyHandle(pid int, handle windows.Handle) (Identity, error) {
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return Identity{}, err
	}
	if code != stillActive {
		return Identity{}, errProcessGone
	}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return Identity{}, err
	}
	boot, err := bootID()
	if err != nil {
		return Identity{}, err
	}
	start := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	return Identity{PID: pid, Start: start, Boot: boot}, nil
}

// The handle pins the process object, so the identity check and the
// termination cannot straddle a PID reuse.
func killProcess(id Identity) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(id.PID))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle) //nolint:errcheck // Closing a query handle cannot fail in a way the caller can act on.
	current, err := identifyHandle(id.PID, handle)
	if err != nil || !id.sameAs(current) {
		return
	}
	_ = windows.TerminateProcess(handle, uint32(killedExitCode)) //nolint:errcheck // A process that is already gone is the goal.
}

// Windows has no process groups a reaper can signal; the Job bounds the tree.
func killGroup(int, Identity) {}
