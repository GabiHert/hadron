// Package planimport reads a planner run folder (sessions/*.md, log.md, questions/<nick>/Q-NN.json
// and gates/<nick>.json) into plain structs. It touches no database: the workspace_import_planner_dir
// action turns the result into a plan and applies it.
package planimport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Folder layout.
const (
	SessionsDir  = "sessions"
	QuestionsDir = "questions"
	GatesDir     = "gates"
	LogFile      = "log.md"
	// MaxMessageChars caps one imported message.
	MaxMessageChars = 8000
	// MaxMessagesPerCard keeps the newest messages of a very long session file.
	MaxMessagesPerCard = 500
)

// Message is one section of a session file.
type Message struct {
	At          time.Time
	FromSession bool   // session → planner (a report); false is planner → session
	Kind        string // the section's kind: progress, question, blocked, done, new, answer ...
	Text        string
}

// Question is one questions/<nick>/Q-NN.json.
type Question struct {
	Seq        int
	Kind       string // decision, unblock or approval
	Text       string
	Context    string
	Options    []Option
	Multi      bool
	Status     string // open, answered or withdrawn
	AskedAt    time.Time
	AnsweredAt time.Time
	Selections []string
	Notes      string
	Via        string
	Approval   json.RawMessage
}

// Option is one choice of a question.
type Option struct{ Label, Description string }

// Gate is one entry of gates/<nick>.json.
type Gate struct {
	Kind    string
	Summary string
	Since   time.Time
}

// Session is everything the folder says about one nickname.
type Session struct {
	Nick            string
	Key             string // from "- task: ACME-12", empty for "none yet"
	ClaudeSessionID string
	Created         time.Time
	Messages        []Message
	Questions       []Question
	Gates           []Gate
	// Target is the last column the log moved it to: planning, backlog, in-progress or done.
	Target string
	// Problems are things in the folder that could not be read, for the plan to show.
	Problems []string
}

// Parsed is a whole folder.
type Parsed struct {
	Dir      string
	Sessions []*Session
	// UnmatchedLog counts log lines about subjects that have no session file.
	UnmatchedLog int
}

var (
	uuidRe    = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	keyRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[0-9]+$`)
	sectionRe = regexp.MustCompile(`^##\s+(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\s+·\s+(session → planner|planner → session)\s+·\s+(\S+)(?:\s+[—–-]+\s+(.*))?$`)
	logRe     = regexp.MustCompile(`^-\s+(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\s+·\s+(.+?)\s+·\s+(.+)$`)
	targetRe  = regexp.MustCompile(`→\s*(planning|backlog|in-progress|done)\b`)
	nickRe    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	qFileRe   = regexp.MustCompile(`^Q-(\d+)\.json$`)
)

const timeLayout = "2006-01-02 15:04"

func parseTime(s string) time.Time {
	for _, layout := range []string{timeLayout, time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.Local); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// ValidNick says whether a nickname from a file name can be a card nickname.
func ValidNick(nick string) bool { return len(nick) <= 60 && nickRe.MatchString(nick) }

// Parse reads the folder. A missing sessions folder is an error; everything else that cannot be
// read is reported on the session it belongs to.
func Parse(dir string) (Parsed, error) { return ParseContext(context.Background(), dir, nil) }

// ParseContext is Parse with a time budget and progress. It stops with ctx's error when ctx ends,
// and calls progress (when not nil) after each session file with how many are done of the total.
func ParseContext(ctx context.Context, dir string, progress func(done, total int)) (Parsed, error) {
	p := Parsed{Dir: dir}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return p, fmt.Errorf("%s is not a folder. Pass the planner run folder (it holds sessions/, log.md, questions/ and gates/)", dir)
	}
	files, err := filepath.Glob(filepath.Join(dir, SessionsDir, "*.md"))
	if err != nil {
		return p, err
	}
	if len(files) == 0 {
		return p, fmt.Errorf("%s has no sessions/*.md files, so there is nothing to import", dir)
	}
	sort.Strings(files)
	byNick := map[string]*Session{}
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return p, fmt.Errorf("reading the planner folder stopped after %d of %d sessions: %w", i, len(files), err)
		}
		nick := strings.TrimSuffix(filepath.Base(f), ".md")
		s := &Session{Nick: nick}
		parseSessionFile(f, s)
		parseQuestions(filepath.Join(dir, QuestionsDir, nick), s)
		parseGates(filepath.Join(dir, GatesDir, nick+".json"), s)
		p.Sessions = append(p.Sessions, s)
		byNick[nick] = s
		if progress != nil {
			progress(i+1, len(files))
		}
	}
	if err := ctx.Err(); err != nil {
		return p, fmt.Errorf("reading the planner folder stopped before log.md: %w", err)
	}
	p.UnmatchedLog = applyLog(filepath.Join(dir, LogFile), byNick)
	return p, nil
}

func parseSessionFile(path string, s *Session) {
	f, err := os.Open(path)
	if err != nil {
		s.Problems = append(s.Problems, err.Error())
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	var cur *Message
	var body []string
	flush := func() {
		if cur == nil {
			return
		}
		text := strings.TrimSpace(strings.Join(body, "\n"))
		if cur.Text != "" && text != "" {
			text = cur.Text + "\n\n" + text
		} else if cur.Text != "" {
			text = cur.Text
		}
		if len([]rune(text)) > MaxMessageChars {
			text = string([]rune(text)[:MaxMessageChars-1]) + "…"
		}
		cur.Text = text
		if cur.Text != "" {
			s.Messages = append(s.Messages, *cur)
		}
		cur, body = nil, nil
	}
	inHeader := true
	for sc.Scan() {
		line := sc.Text()
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			flush()
			inHeader = false
			cur = &Message{At: parseTime(m[1]), FromSession: m[2] == "session → planner", Kind: m[3], Text: strings.TrimSpace(m[4])}
			continue
		}
		if strings.HasPrefix(line, "## ") {
			flush()
			inHeader = false
			continue
		}
		if cur != nil {
			body = append(body, line)
			continue
		}
		if inHeader && strings.HasPrefix(line, "- ") {
			key, val, ok := strings.Cut(strings.TrimPrefix(line, "- "), ":")
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "task":
				if keyRe.MatchString(val) {
					s.Key = strings.ToUpper(val)
				}
			case "claude session":
				if uuidRe.MatchString(val) {
					s.ClaudeSessionID = strings.ToLower(val)
				}
			case "created":
				s.Created = parseTime(val)
			}
		}
	}
	flush()
	if len(s.Messages) > MaxMessagesPerCard {
		s.Messages = s.Messages[len(s.Messages)-MaxMessagesPerCard:]
	}
}

func parseQuestions(dir string, s *Session) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		m := qFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			s.Problems = append(s.Problems, e.Name()+": "+err.Error())
			continue
		}
		var j struct {
			Kind      string          `json:"kind"`
			AskedAt   string          `json:"asked_at"`
			Context   string          `json:"context"`
			Status    string          `json:"status"`
			Approval  json.RawMessage `json:"approval"`
			Questions []struct {
				Question    string `json:"question"`
				MultiSelect bool   `json:"multiSelect"`
				Options     []struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				} `json:"options"`
			} `json:"questions"`
			Answer *struct {
				Selections json.RawMessage `json:"selections"`
				FreeText   string          `json:"free_text"`
				AnsweredAt string          `json:"answered_at"`
				Via        string          `json:"via"`
			} `json:"answer"`
		}
		if err := json.Unmarshal(raw, &j); err != nil {
			s.Problems = append(s.Problems, e.Name()+": not valid JSON: "+err.Error())
			continue
		}
		seq, _ := strconv.Atoi(m[1])
		q := Question{Seq: seq, Kind: j.Kind, Context: strings.TrimSpace(j.Context), Status: "open", AskedAt: parseTime(j.AskedAt), Approval: j.Approval}
		switch q.Kind {
		case "decision", "unblock", "approval":
		default:
			q.Kind = "decision"
		}
		if len(j.Questions) > 0 {
			first := j.Questions[0]
			q.Text, q.Multi = strings.TrimSpace(first.Question), first.MultiSelect
			for _, o := range first.Options {
				q.Options = append(q.Options, Option{Label: o.Label, Description: o.Description})
			}
		}
		if q.Text == "" {
			q.Text, _, _ = strings.Cut(q.Context, "\n")
		}
		if q.Text == "" {
			q.Text = fmt.Sprintf("Q-%02d (no text in the file)", seq)
		}
		switch j.Status {
		case "answered":
			q.Status = "answered"
		case "withdrawn", "cancelled", "canceled":
			q.Status = "withdrawn"
		}
		if j.Answer != nil && q.Status == "answered" {
			q.Selections = flattenSelections(j.Answer.Selections)
			q.Notes, q.Via, q.AnsweredAt = strings.TrimSpace(j.Answer.FreeText), j.Answer.Via, parseTime(j.Answer.AnsweredAt)
		}
		s.Questions = append(s.Questions, q)
	}
	sort.Slice(s.Questions, func(i, j int) bool { return s.Questions[i].Seq < s.Questions[j].Seq })
}

// flattenSelections reads the answer's selections, which are one list of labels per question.
func flattenSelections(raw json.RawMessage) []string {
	var nested [][]string
	if json.Unmarshal(raw, &nested) == nil {
		var out []string
		for _, l := range nested {
			out = append(out, l...)
		}
		return out
	}
	var flat []string
	if json.Unmarshal(raw, &flat) == nil {
		return flat
	}
	return nil
}

func parseGates(path string, s *Session) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var j struct {
		Gates []struct {
			Kind    string `json:"kind"`
			Summary string `json:"summary"`
			Since   string `json:"since"`
		} `json:"gates"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		s.Problems = append(s.Problems, filepath.Base(path)+": not valid JSON: "+err.Error())
		return
	}
	for _, g := range j.Gates {
		switch g.Kind {
		case "merge", "promote", "deploy-confirm", "close":
			s.Gates = append(s.Gates, Gate{Kind: g.Kind, Summary: strings.TrimSpace(g.Summary), Since: parseTime(g.Since)})
		default:
			s.Problems = append(s.Problems, fmt.Sprintf("gate kind %q is not one of merge, promote, deploy-confirm, close, so it was skipped", g.Kind))
		}
	}
}

// applyLog sets each session's Target from the last "→ planning|backlog|in-progress|done" line
// about it, and returns how many log lines named nobody it knows. A subject is "KEY nick", "KEY"
// or "nick".
func applyLog(path string, byNick map[string]*Session) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	byKey := map[string]*Session{}
	for _, s := range byNick {
		if s.Key != "" {
			byKey[s.Key] = s
		}
	}
	unmatched := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		m := logRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		var s *Session
		fields := strings.Fields(m[2])
		switch {
		case len(fields) >= 2:
			s = byNick[fields[len(fields)-1]]
		case len(fields) == 1:
			if s = byNick[fields[0]]; s == nil {
				s = byKey[strings.ToUpper(fields[0])]
			}
		}
		if s == nil {
			unmatched++
			continue
		}
		// Only the transition field counts; later columns are free text.
		field, _, _ := strings.Cut(m[3], " · ")
		if t := targetRe.FindStringSubmatch(field); t != nil {
			s.Target = t[1]
		}
	}
	return unmatched
}

// ErrNothing is returned when a folder parses to no sessions.
var ErrNothing = errors.New("nothing to import")
