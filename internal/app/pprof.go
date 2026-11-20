package app

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/GabiHert/hadron/internal/dlog"
)

// PprofEnv names the variable that turns on Go's profiling endpoints, for measuring memory and
// CPU: HADRON_PPROF=127.0.0.1:6061 serves /debug/pprof/ there. It is off unless set, and only
// listens on the loopback interface, never on the network.
const PprofEnv = "HADRON_PPROF"

// pprofAddr checks the address in HADRON_PPROF: empty is off, and anything but a loopback
// host is refused.
func pprofAddr(v string) (string, bool) {
	if v == "" {
		return "", false
	}
	host, _, err := net.SplitHostPort(v)
	if err != nil {
		return "", false
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", false
	}
	return v, true
}

// startPprof serves the profiling endpoints when HADRON_PPROF asks for them.
func startPprof(log *dlog.Logger) {
	addr, ok := pprofAddr(os.Getenv(PprofEnv))
	if !ok {
		if os.Getenv(PprofEnv) != "" {
			log.Error("pprof: HADRON_PPROF must be a loopback host:port, such as 127.0.0.1:6061", "value", os.Getenv(PprofEnv))
		}
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	log.Info("pprof: serving the profiling endpoints", "addr", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Error("pprof: could not serve", "addr", addr, "error", err.Error())
		}
	}()
}
