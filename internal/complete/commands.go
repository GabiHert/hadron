// Package complete finds what the chat composer offers after "/" and "@": the slash commands a
// Claude Code session knows (built in, the project's, the user's, and user-invocable skills) and
// the files and folders of the session's folder. It only reads, and never leaves the folder it is
// given: symlinks that point outside it are skipped.
package complete

import (
	"bufio"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Sources of a command.
const (
	SourceBuiltin = "built-in"
	SourceProject = "project"
	SourceUser    = "user"
)

// Kinds of a command.
const (
	KindCommand = "command"
	KindSkill   = "skill"
)

// Command is one slash command the composer offers.
type Command struct {
	// Name is the command without its slash; a command in a subfolder is "ns:cmd".
	Name        string `json:"name"`
	Description string `json:"description"`
	// Source is built-in, project or user.
	Source string `json:"source"`
	// Kind is command or skill.
	Kind string `json:"kind"`
}

// Builtins are Claude Code's own slash commands, with one-line descriptions. Keep it to the ones
// Claude Code ships; a command it drops can stay a while, since typing it does no harm.
var Builtins = []Command{
	{Name: "add-dir", Description: "Add a working directory"},
	{Name: "agents", Description: "Manage subagents"},
	{Name: "bug", Description: "Report a bug to Anthropic"},
	{Name: "clear", Description: "Clear the conversation and free up context"},
	{Name: "compact", Description: "Compact the conversation, with optional focus instructions"},
	{Name: "config", Description: "Open the settings"},
	{Name: "context", Description: "Show how the context window is used"},
	{Name: "cost", Description: "Show the token usage and cost of this session"},
	{Name: "doctor", Description: "Check the health of the Claude Code installation"},
	{Name: "effort", Description: "Set the reasoning effort"},
	{Name: "exit", Description: "Exit Claude Code"},
	{Name: "export", Description: "Export the conversation to a file or the clipboard"},
	{Name: "help", Description: "Show help and the available commands"},
	{Name: "hooks", Description: "Manage hooks"},
	{Name: "init", Description: "Write a CLAUDE.md that documents the codebase"},
	{Name: "login", Description: "Sign in to your Anthropic account"},
	{Name: "logout", Description: "Sign out of your Anthropic account"},
	{Name: "mcp", Description: "Manage MCP servers"},
	{Name: "memory", Description: "Edit the CLAUDE.md memory files"},
	{Name: "model", Description: "Choose the model"},
	{Name: "permissions", Description: "View and change tool permissions"},
	{Name: "pr-comments", Description: "Show the comments of a pull request"},
	{Name: "resume", Description: "Resume an earlier conversation"},
	{Name: "review", Description: "Review a pull request"},
	{Name: "rewind", Description: "Rewind the conversation and code to an earlier point"},
	{Name: "status", Description: "Show the version, model, account and connectivity"},
	{Name: "todos", Description: "List the current to-do items"},
	{Name: "usage", Description: "Show plan usage and limits"},
	{Name: "vim", Description: "Toggle vim editing mode"},
}

// Limits of a command scan, so a huge or looping commands folder cannot stall the composer.
const (
	maxCommandFiles = 2000
	maxCommandDepth = 6
	maxFrontmatter  = 64 * 1024
	maxDescription  = 240
)

// Commands lists the commands a session in projectDir can run, given Claude's config folder
// (~/.claude or CLAUDE_CONFIG_DIR). The order is the project's commands and skills, the user's,
// then the built-ins; a name that appears twice keeps its first, most specific, entry.
// projectDir or configDir may be empty.
func Commands(projectDir, configDir string) []Command {
	var all []Command
	if projectDir != "" {
		all = append(all, scanCommands(filepath.Join(projectDir, ".claude", "commands"), SourceProject)...)
		all = append(all, scanSkills(filepath.Join(projectDir, ".claude", "skills"), SourceProject)...)
	}
	if configDir != "" && !sameDir(configDir, filepath.Join(projectDir, ".claude")) {
		all = append(all, scanCommands(filepath.Join(configDir, "commands"), SourceUser)...)
		all = append(all, scanSkills(filepath.Join(configDir, "skills"), SourceUser)...)
	}
	for _, b := range Builtins {
		b.Source, b.Kind = SourceBuiltin, KindCommand
		all = append(all, b)
	}
	seen := map[string]bool{}
	out := make([]Command, 0, len(all))
	for _, c := range all {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	return out
}

// sameDir reports whether two paths are the same folder (a project whose folder is the home folder
// has ~/.claude as its project folder too).
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ra, ea := filepath.EvalSymlinks(a)
	rb, eb := filepath.EvalSymlinks(b)
	if ea != nil || eb != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// scanCommands reads <dir>/**/*.md. A file in a subfolder is named "sub:name".
func scanCommands(dir, source string) []Command {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	var out []Command
	n := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator)) >= maxCommandDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if n++; n > maxCommandFiles {
			return filepath.SkipAll
		}
		if !insideReal(root, path, d) {
			return nil
		}
		meta, body := readFrontmatter(path)
		name := strings.TrimSuffix(rel, filepath.Ext(rel))
		name = strings.ReplaceAll(filepath.ToSlash(name), "/", ":")
		desc := meta["description"]
		if desc == "" {
			desc = firstLine(body)
		}
		out = append(out, Command{Name: name, Description: oneLine(desc), Source: source, Kind: KindCommand})
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// scanSkills reads <dir>/*/SKILL.md. A skill whose frontmatter says user-invocable: false is left
// out: only Claude may run it.
func scanSkills(dir, source string) []Command {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Command
	for i, e := range entries {
		if i >= maxCommandFiles {
			break
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		sub := filepath.Join(root, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(sub)
			if err != nil || !within(root, real) {
				continue
			}
			sub = real
		}
		path := filepath.Join(sub, "SKILL.md")
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		if real, err := filepath.EvalSymlinks(path); err != nil || !within(root, real) {
			continue
		}
		meta, body := readFrontmatter(path)
		if v := strings.ToLower(meta["user-invocable"]); v == "false" || v == "no" {
			continue
		}
		name := meta["name"]
		if name == "" || strings.ContainsAny(name, " /\t") {
			name = e.Name()
		}
		desc := meta["description"]
		if desc == "" {
			desc = firstLine(body)
		}
		out = append(out, Command{Name: name, Description: oneLine(desc), Source: source, Kind: KindSkill})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// insideReal reports whether a walked entry stays inside root once its symlinks are resolved.
func insideReal(root, path string, d fs.DirEntry) bool {
	if d.Type()&fs.ModeSymlink == 0 {
		return true
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	st, err := os.Stat(real)
	return err == nil && !st.IsDir() && within(root, real)
}

// within reports whether path is root or below it. Both must be clean, resolved paths.
func within(root, path string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// readFrontmatter reads the YAML frontmatter of a markdown file as flat key: value pairs, and the
// start of the body. It understands plain, quoted and folded (> or |) scalars, which is all a
// command or skill header uses.
func readFrontmatter(path string) (map[string]string, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	buf := make([]byte, maxFrontmatter)
	n, _ := io.ReadFull(f, buf)
	data := bytes.ReplaceAll(buf[:n], []byte("\r\n"), []byte("\n"))
	meta := map[string]string{}
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return meta, text
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, text
	}
	header, body := rest[:end], rest[end+4:]
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	sc := bufio.NewScanner(strings.NewReader(header))
	sc.Buffer(make([]byte, 0, 4096), maxFrontmatter)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch {
		case v == ">" || v == "|" || v == ">-" || v == "|-" || v == ">+" || v == "|+" || v == "":
			var parts []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t") || lines[i+1] == "") {
				i++
				if s := strings.TrimSpace(lines[i]); s != "" {
					parts = append(parts, s)
				}
			}
			v = strings.Join(parts, " ")
		case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
			v = strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`)
		case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
			v = strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		default:
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
		}
		meta[k] = v
	}
	return meta, body
}

// firstLine is the first line of a body that is not blank or a heading marker alone.
func firstLine(body string) string {
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#"))
		if l != "" {
			return l
		}
	}
	return ""
}

// oneLine collapses whitespace and cuts a description to one short line.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxDescription {
		s = strings.TrimSpace(string(r[:maxDescription-1])) + "…"
	}
	return s
}
