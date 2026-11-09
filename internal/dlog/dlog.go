// Package dlog is Hadron's own log: JSON lines under $HADRON_HOME/logs, one file a day. It is off
// at debug level by default (only errors and recovered panics are written) and never takes
// message text, secrets or tokens: callers pass field names and counts, and the logger refuses
// fields whose name suggests a secret or a body.
package dlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Levels, in order.
const (
	Debug = "debug"
	Info  = "info"
	Warn  = "warn"
	Error = "error"
)

var rank = map[string]int{Debug: 0, Info: 1, Warn: 2, Error: 3}

// Limits.
const (
	Retention   = 7 * 24 * time.Hour
	MaxTotal    = 50 << 20
	maxFieldLen = 500
	filePrefix  = "hadron-"
	fileSuffix  = ".jsonl"
)

// Line is one parsed log line.
type Line struct {
	Time   string         `json:"time"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
	Raw    string         `json:"raw"`
}

// Logger writes the log. A nil *Logger is a working logger that writes nothing.
type Logger struct {
	dir   string
	debug atomic.Bool
	now   func() time.Time

	mu     sync.Mutex
	day    string
	file   *os.File
	size   int64
	writes int
	capped bool
}

// New returns a logger that writes to dir. It prunes old files at once.
func New(dir string) *Logger {
	l := &Logger{dir: dir, now: time.Now}
	l.mu.Lock()
	l.pruneLocked()
	l.mu.Unlock()
	return l
}

// Dir is the folder the log files are in.
func (l *Logger) Dir() string {
	if l == nil {
		return ""
	}
	return l.dir
}

// SetDebug turns debug logging on or off.
func (l *Logger) SetDebug(on bool) {
	if l != nil {
		l.debug.Store(on)
	}
}

// Debugging says whether debug logging is on.
func (l *Logger) Debugging() bool { return l != nil && l.debug.Load() }

// Close closes the open file.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

var secretish = regexp.MustCompile(`(?i)(secret|token|password|passwd|authorization|auth|apikey|api_key|bearer|cookie|credential|text|body|prompt|message|content|html|source)`)

// Fields turns alternating keys and values into the fields map of a line, redacting the values
// of any key that looks like a secret or a message body, and cutting long strings.
func fields(kv []any) map[string]any {
	if len(kv) == 0 {
		return nil
	}
	out := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k := fmt.Sprint(kv[i])
		v := kv[i+1]
		if secretish.MatchString(k) && !strings.HasSuffix(k, "_chars") && !strings.HasSuffix(k, "_keys") && !strings.HasSuffix(k, "_count") {
			out[k] = "[redacted]"
			continue
		}
		switch x := v.(type) {
		case error:
			v = cut(x.Error())
		case string:
			v = cut(x)
		case time.Duration:
			v = x.Milliseconds()
		case []string:
			c := make([]string, len(x))
			for j, s := range x {
				c[j] = cut(s)
			}
			v = c
		}
		out[k] = v
	}
	return out
}

func cut(s string) string {
	if len(s) <= maxFieldLen {
		return s
	}
	return s[:maxFieldLen] + "…"
}

// Log writes a line at a level. Below error it is written only when debug logging is on.
func (l *Logger) Log(level, msg string, kv ...any) {
	if l == nil {
		return
	}
	if rank[level] < rank[Error] && !l.debug.Load() {
		return
	}
	entry := map[string]any{"time": l.now().UTC().Format(time.RFC3339Nano), "level": level, "msg": cut(msg)}
	for k, v := range fields(kv) {
		if k != "time" && k != "level" && k != "msg" {
			entry[k] = v
		}
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.write(append(b, '\n'))
}

// Debug, Info, Warn and Error write a line at that level.
func (l *Logger) Debug(msg string, kv ...any) { l.Log(Debug, msg, kv...) }
func (l *Logger) Info(msg string, kv ...any)  { l.Log(Info, msg, kv...) }
func (l *Logger) Warn(msg string, kv ...any)  { l.Log(Warn, msg, kv...) }
func (l *Logger) Error(msg string, kv ...any) { l.Log(Error, msg, kv...) }

// Panic records a recovered panic with its stack. It is always written.
func (l *Logger) Panic(where string, recovered any) {
	if l == nil {
		return
	}
	l.Log(Error, "panic recovered", "where", where, "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
}

func (l *Logger) write(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	day := l.now().Format("2006-01-02")
	if l.file == nil || l.day != day {
		if l.file != nil {
			_ = l.file.Close()
			l.file = nil
		}
		if err := os.MkdirAll(l.dir, 0o700); err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(l.dir, filePrefix+day+fileSuffix), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		st, _ := f.Stat()
		l.file, l.day, l.size, l.capped = f, day, 0, false
		if st != nil {
			l.size = st.Size()
		}
		l.pruneLocked()
	}
	if l.size+int64(len(b)) > MaxTotal {
		if !l.capped {
			l.capped = true
			note := fmt.Sprintf(`{"time":%q,"level":"warn","msg":"log cap reached: further lines are dropped until tomorrow"}`+"\n", l.now().UTC().Format(time.RFC3339Nano))
			_, _ = l.file.WriteString(note)
		}
		return
	}
	n, _ := l.file.Write(b)
	l.size += int64(n)
	if l.writes++; l.writes%2000 == 0 {
		l.pruneLocked()
	}
}

// Prune applies the retention and the size cap now.
func (l *Logger) Prune() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()
}

type logFile struct {
	path string
	day  time.Time
	size int64
}

func (l *Logger) files() []logFile {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil
	}
	var out []logFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), fileSuffix), time.Local)
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, logFile{filepath.Join(l.dir, name), day, info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].day.Before(out[j].day) })
	return out
}

func (l *Logger) pruneLocked() {
	files := l.files()
	cutoff := l.now().Add(-Retention)
	var keep []logFile
	for _, f := range files {
		// A file for day D holds D's lines, so it goes once D's end is past the retention.
		if f.day.Add(24 * time.Hour).Before(cutoff) {
			_ = os.Remove(f.path)
			continue
		}
		keep = append(keep, f)
	}
	var total int64
	for _, f := range keep {
		total += f.size
	}
	for i := 0; total > MaxTotal && i < len(keep)-1; i++ {
		_ = os.Remove(keep[i].path)
		total -= keep[i].size
	}
}

// Tail returns the newest n lines at or above level that contain the text, oldest first. n 0 means 200.
func (l *Logger) Tail(n int, level, contains string) []Line {
	if l == nil {
		return []Line{}
	}
	if n <= 0 {
		n = 200
	}
	min := rank[level]
	files := l.files()
	var out []Line
	for i := len(files) - 1; i >= 0 && len(out) < n; i-- {
		var lines []Line
		f, err := os.Open(files[i].path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for sc.Scan() {
			ln := parseLine(sc.Text())
			if rank[ln.Level] < min || (contains != "" && !strings.Contains(strings.ToLower(ln.Raw), strings.ToLower(contains))) {
				continue
			}
			lines = append(lines, ln)
		}
		f.Close()
		out = append(lines, out...)
		if len(out) > n {
			out = out[len(out)-n:]
		}
	}
	if out == nil {
		out = []Line{}
	}
	return out
}

func parseLine(raw string) Line {
	ln := Line{Raw: raw, Level: Info}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		ln.Msg = raw
		return ln
	}
	ln.Time, _ = m["time"].(string)
	if lv, ok := m["level"].(string); ok {
		ln.Level = lv
	}
	ln.Msg, _ = m["msg"].(string)
	delete(m, "time")
	delete(m, "level")
	delete(m, "msg")
	if len(m) > 0 {
		ln.Fields = m
	}
	return ln
}
