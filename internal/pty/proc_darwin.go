//go:build darwin

package pty

/*
#include <libproc.h>
#include <sys/proc_info.h>
#include <string.h>

static int hadron_proc_cwd(int pid, char *buf, int n) {
	struct proc_vnodepathinfo vpi;
	int r = proc_pidinfo(pid, PROC_PIDVNODEPATHINFO, 0, &vpi, sizeof(vpi));
	if (r <= 0) return -1;
	strlcpy(buf, vpi.pvi_cdir.vip_path, n);
	return 0;
}
*/
import "C"

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ProcessCwd returns the current folder of a process, or "" when it cannot be read.
func ProcessCwd(pid int) string {
	if pid <= 0 {
		return ""
	}
	buf := make([]byte, 1024)
	if C.hadron_proc_cwd(C.int(pid), (*C.char)(unsafe.Pointer(&buf[0])), C.int(len(buf))) != 0 {
		return ""
	}
	return strings.TrimRight(string(buf[:strings.IndexByte(string(buf), 0)]), "\x00")
}

// foreground returns the name and pid of the process group leader that holds the terminal of the
// process pid (the program the user is running in a shell), or the process itself.
func foreground(pid int) (string, int) {
	if pid <= 0 {
		return "", 0
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", 0
	}
	fg := int(kp.Eproc.Tpgid)
	if fg > 0 && fg != pid {
		if fk, err := unix.SysctlKinfoProc("kern.proc.pid", fg); err == nil {
			return commName(fk.Proc.P_comm[:]), fg
		}
	}
	return commName(kp.Proc.P_comm[:]), pid
}

func commName(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}
