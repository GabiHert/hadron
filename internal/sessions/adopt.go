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

	"github.com/GabiHert/hadron/internal/domain"
)

// MinAdoptableSize skips session files too small to hold a conversation.
const MinAdoptableSize = 2048

// Adoptable is a Claude Code conversation on disk that Hadron could take over.
type Adoptable struct {
	ClaudeSessionID  string    `json:"claude_session_id"`
	FirstUserMessage string    `json:"first_user_message"`
	LastActivity     time.Time `json:"last_activity"`
	Size             int64     `json:"size"`
	// Cwd is the folder the conversation was run in, read from its transcript. Resuming it works
	// only in that folder.
	Cwd string `json:"cwd"`
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

// ListAdoptable scans the conversations that were run in one folder, newest first. Files under
// 2 KB are skipped. A transcript that records no cwd is taken to be of that folder.
func ListAdoptable(configDir, root string) ([]Adoptable, error) {
	var out []Adoptable
	for _, dir := range ProjectDirs(configDir, root) {
		out = append(out, scanProjectDir(dir, filepath.Clean(root))...)
	}
	sortNewest(out)
	return out, nil
}

// ListAllAdoptable scans every folder under <configDir>/projects, newest first, with each
// conversation's real cwd read from its transcript. The folder names are not decoded: the
// encoding turns every non-alphanumeric character into a dash, so it cannot be reversed. A
// transcript that records no cwd is left out, because Hadron would not know where to resume it.
func ListAllAdoptable(configDir string) ([]Adoptable, error) {
	entries, err := os.ReadDir(filepath.Join(configDir, "projects"))
	if err != nil {
		if os.IsNotExist(err) {
			return []Adoptable{}, nil
		}
		return nil, err
	}
	out := []Adoptable{}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, scanProjectDir(filepath.Join(configDir, "projects", e.Name()), "")...)
		}
	}
	sortNewest(out)
	return out, nil
}

func sortNewest(list []Adoptable) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].LastActivity.After(list[j].LastActivity) })
}

// scanProjectDir lists the adoptable conversations of one projects/ folder. fallbackCwd is used
// for a transcript without a cwd; when it is empty, such a transcript is skipped.
func scanProjectDir(dir, fallbackCwd string) []Adoptable {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Adoptable
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() < MinAdoptableSize {
			continue
		}
		first, cwd := readTranscript(filepath.Join(dir, e.Name()))
		if cwd == "" {
			cwd = fallbackCwd
		}
		if cwd == "" {
			continue
		}
		out = append(out, Adoptable{
			ClaudeSessionID:  strings.TrimSuffix(e.Name(), ".jsonl"),
			FirstUserMessage: first,
			LastActivity:     info.ModTime().UTC(),
			Size:             info.Size(),
			Cwd:              cwd,
		})
	}
	return out
}

// Folder is the folder a session runs in before any worktree: the card's own folder, else (for
// the planner, card nil) the workspace's planner folder, else the workspace root.
func Folder(ws domain.Workspace, card *domain.Card) string {
	if card != nil && card.Cwd != "" {
		return card.Cwd
	}
	if card == nil && ws.PlannerCwd != "" {
		return ws.PlannerCwd
	}
	return ws.Root
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

// readTranscript returns the first thing the user typed, cut to 200 characters, and the folder the
// conversation was run in (the first cwd a line records). Tool results, slash-command bookkeeping
// and sidechains are not the user typing. It reads at most 400 lines.
func readTranscript(path string) (first, cwd string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for i := 0; i < 400 && sc.Scan() && (first == "" || cwd == ""); i++ {
		var line struct {
			Type        string `json:"type"`
			Cwd         string `json:"cwd"`
			IsMeta      bool   `json:"isMeta"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if cwd == "" && filepath.IsAbs(line.Cwd) {
			cwd = filepath.Clean(line.Cwd)
		}
		if first != "" || line.Type != "user" || line.IsMeta || line.IsSidechain {
			continue
		}
		text := userText(line.Message.Content)
		if text == "" || strings.HasPrefix(text, "<command-") || strings.HasPrefix(text, "<local-command") || strings.HasPrefix(text, "Caveat:") {
			continue
		}
		first = clip(strings.Join(strings.Fields(text), " "), 200)
	}
	return first, cwd
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
