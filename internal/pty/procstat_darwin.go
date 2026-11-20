//go:build darwin

package pty

/*
#include <libproc.h>
#include <sys/resource.h>
#include <mach/mach_time.h>

// hadron_proc_cpu returns a process's user plus system CPU time in nanoseconds, or -1.
static long long hadron_proc_cpu(int pid) {
	struct rusage_info_v0 ri;
	if (proc_pid_rusage(pid, RUSAGE_INFO_V0, (rusage_info_t *)&ri) != 0) return -1;
	static mach_timebase_info_data_t tb;
	if (tb.denom == 0) mach_timebase_info(&tb);
	unsigned long long t = ri.ri_user_time + ri.ri_system_time;
	return (long long)(t * tb.numer / tb.denom);
}
*/
import "C"

import (
	"time"

	"golang.org/x/sys/unix"
)

// Descendants lists every process below pid (children, their children, and so on).
func Descendants(pid int) []int {
	if pid <= 0 {
		return nil
	}
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	kids := map[int][]int{}
	for _, p := range all {
		kids[int(p.Eproc.Ppid)] = append(kids[int(p.Eproc.Ppid)], int(p.Proc.P_pid))
	}
	var out []int
	queue := []int{pid}
	seen := map[int]bool{pid: true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, k := range kids[cur] {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
				queue = append(queue, k)
			}
		}
	}
	return out
}

// ProcessCPU is the CPU time (user and system) a process has used, or -1 when it cannot be read.
func ProcessCPU(pid int) time.Duration {
	n := C.hadron_proc_cpu(C.int(pid))
	if n < 0 {
		return -1
	}
	return time.Duration(n)
}

// MemoryPressure is the system's memory pressure level as macOS reports it: PressureNormal,
// PressureWarning or PressureCritical, or 0 when it cannot be read.
func MemoryPressure() int {
	v, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level")
	if err != nil {
		return 0
	}
	return int(v)
}
