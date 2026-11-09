package sessions

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// MinAdoptableSize skips session files too small to hold a conversation.
const MinAdoptableSize = 2048

// Adoptable is a Claude Code conversation on disk that Hadron could take over.
type Adoptable struct {
	ClaudeSessionID  string    `json:"claude_session_id"`
	FirstUserMessage string    `json:"first_user_message"`
	LastActivity     time.Time `json:"last_activity"`
	Size             int64     `json:"size"`
}

// ClaudeConfigDir is where Claude Code keeps its data: CLAUDE_CONFIG_DIR, else ~/.claude.
func ClaudeConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// EncodeProjectDir is the folder name Claude Code uses under projects/ for a working directory:
// every character that is not an ASCII letter or digit becomes a dash, so /Users/me/my.app becomes
// -Users-me-my-app.
func EncodeProjectDir(cwd string) string { return nonAlnum.ReplaceAllString(cwd, "-") }

// ProjectDirs lists the folders that may hold the conversations of a workspace root: the root as
// given and with symlinks resolved, because Claude records the real path.
func ProjectDirs(configDir, root string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		d := filepath.Join(configDir, "projects", EncodeProjectDir(p))
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(filepath.Clean(root))
	if real, err := filepath.EvalSymlinks(root); err == nil {
		add(real)
	}
	return out
}

// ListAdoptable scans the conversations of a workspace root, newest first. Files under 2 KB are
// skipped.
func ListAdoptable(configDir, root string) ([]Adoptable, error) {
	var out []Adoptable
	for _, dir := range ProjectDirs(configDir, root) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil || info.Size() < MinAdoptableSize {
				continue
			}
			out = append(out, Adoptable{
				ClaudeSessionID:  strings.TrimSuffix(e.Name(), ".jsonl"),
				FirstUserMessage: firstUserMessage(filepath.Join(dir, e.Name())),
				LastActivity:     info.ModTime().UTC(),
				Size:             info.Size(),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastActivity.After(out[j].LastActivity) })
	return out, nil
}

// HasConversation says whether the conversation file of a session id exists for a root.
func HasConversation(configDir, root, id string) bool {
	if !regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`).MatchString(id) {
		return false
	}
	for _, dir := range ProjectDirs(configDir, root) {
		if st, err := os.Stat(filepath.Join(dir, id+".jsonl")); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// firstUserMessage returns the first thing the user typed, cut to 200 characters. Tool results,
// slash-command bookkeeping and sidechains are not the user typing.
func firstUserMessage(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for i := 0; i < 400 && sc.Scan(); i++ {
		var line struct {
			Type        string `json:"type"`
			IsMeta      bool   `json:"isMeta"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "user" || line.IsMeta || line.IsSidechain {
			continue
		}
		text := userText(line.Message.Content)
		if text == "" || strings.HasPrefix(text, "<command-") || strings.HasPrefix(text, "<local-command") || strings.HasPrefix(text, "Caveat:") {
			continue
		}
		return clip(strings.Join(strings.Fields(text), " "), 200)
	}
	return ""
}

func userText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	}
	return ""
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
