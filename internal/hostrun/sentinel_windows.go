//go:build windows

package hostrun

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errSentinelArgs = errors.New("a sentinel needs its two pipe handles in argv")

// The pipes travel as handle numbers in argv; Windows has no fd 3 and 4.
func sentinelArgs(controlRead, statusWrite *os.File) []string {
	return []string{strconv.FormatUint(uint64(controlRead.Fd()), 10), strconv.FormatUint(uint64(statusWrite.Fd()), 10)}
}

// attachPipes marks the two handles inheritable and lists them, since Go
// restricts inheritance to the listed handles. The sentinel gets its own
// console process group so a Ctrl+C aimed at the owner never reaches it.
func attachPipes(sentinel *exec.Cmd, controlRead, statusWrite *os.File) {
	handles := []syscall.Handle{syscall.Handle(controlRead.Fd()), syscall.Handle(statusWrite.Fd())}
	for _, handle := range handles {
		_ = windows.SetHandleInformation(windows.Handle(handle), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT) //nolint:errcheck // A handle that stays uninheritable fails the sentinel start, which reports it.
	}
	sentinel.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:              windows.CREATE_NEW_PROCESS_GROUP,
		AdditionalInheritedHandles: handles,
	}
}

func sentinelPipes(args []string) (control, status *os.File, err error) {
	if len(args) != 2 {
		return nil, nil, errSentinelArgs
	}
	controlHandle, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	statusHandle, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(controlHandle), "control"), os.NewFile(uintptr(statusHandle), "status"), nil
}

func releaseOwnerPipes() {
	_ = os.Stdin.Close()  //nolint:errcheck // The Reviewer holds its own duplicates.
	_ = os.Stdout.Close() //nolint:errcheck // Same.
	_ = os.Stderr.Close() //nolint:errcheck // Same.
}

type platformTree struct {
	job windows.Handle
}

// newPlatformTree creates a Job with KILL_ON_JOB_CLOSE and puts the sentinel
// in it before the Reviewer exists, so the Reviewer and all its descendants
// are born inside. When the sentinel is already in a Job that forbids
// nesting, the tree degrades to the Reviewer process alone.
func newPlatformTree() (platformTree, TreeMode, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return platformTree{}, TreeJobDegraded, nil
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, windows.CurrentProcess())
	}
	if err != nil {
		_ = windows.CloseHandle(job) //nolint:errcheck // The degraded mode no longer needs the handle.
		return platformTree{}, TreeJobDegraded, nil
	}
	return platformTree{job: job}, TreeJob, nil
}

func (t *tree) attach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

func (t *tree) started(pid int) {
	t.pid = pid
}

// terminate delivers Ctrl+Break to the Reviewer's console group, the closest
// Windows comes to SIGTERM.
func (t *tree) terminate() {
	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(t.pid)) //nolint:errcheck // The kill phase follows for a Reviewer that ignores Ctrl+Break.
}

func (t *tree) kill() {
	if t.job == 0 {
		terminatePID(uint32(t.pid))
		return
	}
	self := uint32(os.Getpid())
	for _, pid := range t.jobMembers() {
		if pid != self {
			terminatePID(pid)
		}
	}
}

// sweep is a kill: the Job also closes with the sentinel, which kills any
// member the enumeration missed.
func (t *tree) sweep() {
	t.kill()
}

const jobMemberLimit = 1024

// JOBOBJECT_BASIC_PROCESS_ID_LIST from winnt.h; x/sys does not define it.
type jobProcessIDList struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
	ProcessIDList             [jobMemberLimit]uintptr
}

func (t *tree) jobMembers() []uint32 {
	var list jobProcessIDList
	err := windows.QueryInformationJobObject(t.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil)
	if err != nil && !errors.Is(err, windows.ERROR_MORE_DATA) {
		return nil
	}
	members := make([]uint32, 0, list.NumberOfProcessIdsInList)
	for _, pid := range list.ProcessIDList[:list.NumberOfProcessIdsInList] {
		members = append(members, uint32(pid))
	}
	return members
}

func terminatePID(pid uint32) {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return
	}
	_ = windows.TerminateProcess(handle, uint32(killedExitCode)) //nolint:errcheck // A process that is already gone is the goal.
	_ = windows.CloseHandle(handle)                              //nolint:errcheck // The process handle carries nothing a late close could lose.
}

func exitStatus(state *os.ProcessState) int {
	return state.ExitCode()
}
