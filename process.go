package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/shirou/gopsutil/v3/process"
)

// matchProcess returns the first running process whose command-line contains
// `substr` (case-insensitive). The watchdog's own PID is excluded.
func matchProcess(substr string) (alive bool, pidStr string) {
	if substr == "" {
		return false, ""
	}
	self := int32(os.Getpid())
	procs, err := process.Processes()
	if err != nil {
		return false, ""
	}
	needle := strings.ToLower(substr)
	for _, p := range procs {
		if p.Pid == self {
			continue
		}
		cmd, err := p.Cmdline()
		if err != nil || cmd == "" {
			continue
		}
		if strings.Contains(strings.ToLower(cmd), needle) {
			return true, fmt.Sprintf("%d", p.Pid)
		}
	}
	return false, ""
}