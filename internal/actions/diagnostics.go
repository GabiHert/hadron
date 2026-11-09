package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/GabiHert/hadron/internal/dlog"
)

// SettingDebugLogging is the setting that turns debug logging on.
const SettingDebugLogging = "debug.logging"

// ClientLogIn is the input of client_log.
type ClientLogIn struct {
	Level   string `json:"level" jsonschema:"error, warn, info or debug"`
	Message string `json:"message" jsonschema:"what went wrong in the window"`
	Stack   string `json:"stack,omitempty" jsonschema:"the stack trace, if there is one"`
}

// LogsTailIn is the input of logs_tail.
type LogsTailIn struct {
	Lines    int    `json:"lines,omitempty" jsonschema:"how many of the newest lines (default 200, at most 2000)"`
	Level    string `json:"level,omitempty" jsonschema:"only this level and above: debug, info, warn or error"`
	Contains string `json:"contains,omitempty" jsonschema:"only lines that contain this text, ignoring case"`
}

// DebugLoggingSetIn is the input of debug_logging_set.
type DebugLoggingSetIn struct {
	Enabled bool `json:"enabled" jsonschema:"true writes debug-level lines to the log; false keeps only errors and panics"`
}

// DiagnosticsOut is the result of diagnostics_get.
type DiagnosticsOut struct {
	Version        string         `json:"version"`
	MacOS          string         `json:"macos"`
	Arch           string         `json:"arch"`
	Home           string         `json:"home"`
	LogDir         string         `json:"log_dir"`
	DebugLogging   bool           `json:"debug_logging"`
	Actions        int            `json:"actions"`
	Settings       map[string]any `json:"settings"`
	RecentLogLines []string       `json:"recent_log_lines"`
}

// DebugLoggingOut is the result of debug_logging_set.
type DebugLoggingOut struct {
	Enabled bool   `json:"enabled"`
	LogDir  string `json:"log_dir"`
}

var sensitiveSetting = regexp.MustCompile(`(?i)(token|secret|password|credential|key)`)

func logger(c *Call) (*dlog.Logger, error) {
	if c.Deps.Log == nil {
		return nil, errors.New("logging is not available in this build of the app")
	}
	return c.Deps.Log, nil
}

func macOSVersion() string {
	if out, err := exec.Command("/usr/bin/sw_vers", "-productVersion").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "unknown"
}

// SafeSettings is every setting without the ones that could hold a secret or a user's own config.
func safeSettings(c *Call) map[string]any {
	out := map[string]any{}
	all, err := c.Store.Settings.All()
	if err != nil {
		return out
	}
	for k, v := range all {
		if sensitiveSetting.MatchString(k) || strings.HasPrefix(k, "integration_config:") || strings.HasPrefix(k, "script_approved_perms:") || strings.HasPrefix(k, "custom_fields:") {
			continue
		}
		var val any
		if err := jsonUnmarshalAny(v, &val); err == nil {
			out[k] = val
		}
	}
	return out
}

func registerDiagnostics(r *Registry) {
	Register(r, Spec{Name: "client_log", Description: "Record an error from the app window in Hadron's log (the window calls this from its error handlers). Errors are always kept; lower levels only while debug logging is on."},
		func(ctx context.Context, c *Call, in ClientLogIn) (struct{}, error) {
			l, err := logger(c)
			if err != nil {
				return struct{}{}, nil // the window must never see a failure for reporting one
			}
			switch in.Level {
			case dlog.Error, dlog.Warn, dlog.Info, dlog.Debug:
			default:
				return struct{}{}, fmt.Errorf("level %q must be error, warn, info or debug", in.Level)
			}
			l.Log(in.Level, "window", "detail", in.Message, "stack", in.Stack)
			return struct{}{}, nil
		})

	Register(r, Spec{Name: "logs_tail", Description: "Read the newest lines of Hadron's own log (JSON lines under the logs folder in the data folder), oldest first. Without debug logging the log holds only errors and recovered panics. Secrets and message text are never logged."},
		func(ctx context.Context, c *Call, in LogsTailIn) ([]dlog.Line, error) {
			l, err := logger(c)
			if err != nil {
				return nil, err
			}
			if in.Lines < 0 || in.Lines > 2000 {
				return nil, fmt.Errorf("lines must be between 1 and 2000, got %d", in.Lines)
			}
			if in.Level != "" && in.Level != dlog.Debug && in.Level != dlog.Info && in.Level != dlog.Warn && in.Level != dlog.Error {
				return nil, fmt.Errorf("level %q must be debug, info, warn or error", in.Level)
			}
			return l.Tail(in.Lines, in.Level, in.Contains), nil
		})

	Register(r, Spec{Name: "diagnostics_get", Description: "Everything useful in a bug report: the version, macOS version, data and log folders, whether debug logging is on, the settings that cannot hold a secret, and the last 300 lines of the log."},
		func(ctx context.Context, c *Call, in struct{}) (DiagnosticsOut, error) {
			out := DiagnosticsOut{Version: c.Deps.Version, MacOS: macOSVersion(), Arch: runtime.GOARCH, Home: c.Deps.Home, Settings: safeSettings(c), RecentLogLines: []string{}}
			if r != nil {
				out.Actions = r.Len()
			}
			if l := c.Deps.Log; l != nil {
				out.LogDir, out.DebugLogging = l.Dir(), l.Debugging()
				for _, ln := range l.Tail(300, "", "") {
					out.RecentLogLines = append(out.RecentLogLines, ln.Raw)
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: "debug_logging_set", Description: "Turn debug logging on or off. On, Hadron writes JSON lines at debug level to its log folder (7 days kept, 50 MB at most); off, only errors and recovered panics. Secrets, tokens and message text are never written either way.",
		Mutates: true, Entities: []string{"settings"}},
		func(ctx context.Context, c *Call, in DebugLoggingSetIn) (DebugLoggingOut, error) {
			if err := userOrPlanner(c, "change logging"); err != nil {
				return DebugLoggingOut{}, err
			}
			l, err := logger(c)
			if err != nil {
				return DebugLoggingOut{}, err
			}
			if err := c.Store.Settings.Set(SettingDebugLogging, jsonRaw(in.Enabled)); err != nil {
				return DebugLoggingOut{}, err
			}
			c.AfterCommit(func() { l.SetDebug(in.Enabled) })
			return DebugLoggingOut{Enabled: in.Enabled, LogDir: l.Dir()}, nil
		})
}

func jsonRaw(v any) []byte { b, _ := json.Marshal(v); return b }

func jsonUnmarshalAny(raw []byte, v *any) error { return json.Unmarshal(raw, v) }
