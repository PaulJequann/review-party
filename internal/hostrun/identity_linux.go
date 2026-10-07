//go:build linux

package hostrun

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
)

var bootID = sync.OnceValues(func() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
})

// Field numbers follow proc(5): state 3, ppid 4, pgrp 5, starttime 22.
type procStat struct {
	state byte
	ppid  int
	pgrp  int
	start uint64
}

var errMalformedStat = errors.New("malformed /proc stat line")

func readProcStat(pid int) (procStat, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return procStat{}, err
	}
	// comm may contain spaces and parentheses, so fields start after the last ')'.
	text := string(data)
	last := strings.LastIndex(text, ")")
	if last < 0 {
		return procStat{}, errMalformedStat
	}
	fields := strings.Fields(text[last+1:])
	if len(fields) < 20 {
		return procStat{}, errMalformedStat
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return procStat{}, err
	}
	pgrp, err := strconv.Atoi(fields[2])
	if err != nil {
		return procStat{}, err
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return procStat{}, err
	}
	return procStat{state: fields[0][0], ppid: ppid, pgrp: pgrp, start: start}, nil
}

func identify(pid int) (Identity, error) {
	stat, err := readProcStat(pid)
	if err != nil {
		return Identity{}, err
	}
	if stat.state == 'Z' {
		return Identity{}, errProcessGone
	}
	boot, err := bootID()
	if err != nil {
		return Identity{}, err
	}
	return Identity{PID: pid, Start: stat.start, Boot: boot}, nil
}
