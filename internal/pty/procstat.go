package pty

import "time"

// The memory pressure levels macOS reports (kern.memorystatus_vm_pressure_level).
const (
	PressureNormal   = 1
	PressureWarning  = 2
	PressureCritical = 4
)

// PressureName names a level for people: normal, warning, critical, or unknown.
func PressureName(level int) string {
	switch {
	case level >= PressureCritical:
		return "critical"
	case level >= PressureWarning:
		return "warning"
	case level == PressureNormal:
		return "normal"
	}
	return "unknown"
}

// ChildrenBusy reports whether any process below pid used at least min CPU time during sample:
// a test, a build or a server doing work under a session that itself looks idle. It reads the
// processes twice, sample apart; a process that starts in between counts with all of its time.
func ChildrenBusy(pid int, sample, min time.Duration) bool {
	before := map[int]time.Duration{}
	for _, k := range Descendants(pid) {
		if t := ProcessCPU(k); t >= 0 {
			before[k] = t
		}
	}
	time.Sleep(sample)
	var used time.Duration
	for _, k := range Descendants(pid) {
		t := ProcessCPU(k)
		if t < 0 {
			continue
		}
		used += t - before[k]
	}
	return used >= min
}
