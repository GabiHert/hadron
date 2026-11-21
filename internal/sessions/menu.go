package sessions

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Menu is one of Claude Code's interactive menus as a session's screen shows it: an MCP server
// approval, the trust-this-folder prompt, a tool permission prompt, an AskUserQuestion question
// (one choice or several), the plan approval, or a select list such as /model.
type Menu struct {
	// Kind is mcp, trust, permission, plan, question, review or select.
	Kind string `json:"kind"`
	// Header is the question tab line of an AskUserQuestion, such as "☐ Database".
	Header string `json:"header,omitempty"`
	// Title is the question; Body is the other text above the options.
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	// Options are the rows in the order the arrow keys move through them.
	Options []MenuOption `json:"options"`
	// Cursor is the index of the highlighted option.
	Cursor int `json:"cursor"`
	// Multi is true when options carry check boxes and several may be picked.
	Multi bool `json:"multi"`
	// Footer is the key hint line, such as "Enter to confirm · Esc to cancel".
	Footer string `json:"footer,omitempty"`
	// Cancel is true when Esc closes the menu.
	Cancel bool `json:"cancel"`
	// Sig identifies the menu across redraws: the cursor, check boxes and typed text are not in it.
	Sig string `json:"sig"`
}

// MenuOption is one row of a Menu.
type MenuOption struct {
	// Number is the row's number on screen, 0 when it has none.
	Number      int    `json:"number,omitempty"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Checked is the check box of a multi-select row.
	Checked bool `json:"checked,omitempty"`
	// FreeText marks the row that takes typed text ("Type something.").
	FreeText bool `json:"free_text,omitempty"`
	// Submit marks the Submit row of a multi-select question.
	Submit bool `json:"submit,omitempty"`
	// Box is true for a row with a check box.
	Box bool `json:"box,omitempty"`
}

var (
	ruleLine   = regexp.MustCompile(`^\s*[╭╰┌└]?[─━]{8,}[╮╯┐┘]?\s*$`)
	columns    = regexp.MustCompile(`\s{2,}`)
	dashedLine = regexp.MustCompile(`^\s*[╌┄┈-]{8,}\s*$`)
	numbered   = regexp.MustCompile(`^(\d{1,2})\.\s+(.*)$`)
	checkBox   = regexp.MustCompile(`^\[([ ✔✓xX×])\]\s*(.*)$`)
	headerTabs = regexp.MustCompile(`[☐☒✔]\s+\S`)
	footerHint = regexp.MustCompile(`(?i)\b(enter to (confirm|select|submit|continue)|esc to (cancel|exit|go back|reject)|to navigate)\b`)
)

// cursorMark is the arrow Claude Code draws on the highlighted row.
const cursorMark = '❯'

// ParseMenu finds an open Claude Code menu on a screen. cols is the terminal width, used to tell a
// wrapped row from a description; 0 means unknown. It returns nil when no menu is open.
func ParseMenu(screen string, cols int) *Menu {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	for i, l := range lines {
		lines[i] = unbox(l)
	}
	cur, mark := -1, -1
	for i := len(lines) - 1; i >= 0; i-- {
		if c := runeIndex(lines[i], cursorMark); c >= 0 && strings.TrimSpace(lines[i][:byteAt(lines[i], c)]) == "" {
			cur, mark = i, c
			break
		}
	}
	if cur < 0 {
		return nil
	}
	// Claude's own input box: the arrow sits right under a rule. A menu never draws one there.
	if cur > 0 && ruleLine.MatchString(lines[cur-1]) {
		return nil
	}
	isRow := func(l string) bool {
		if strings.TrimSpace(l) == "" || ruleLine.MatchString(l) || footerHint.MatchString(l) {
			return false
		}
		ind := indent(l)
		if ind < mark {
			return false
		}
		if ind <= mark+3 && numbered.MatchString(strings.TrimSpace(stripMark(l))) {
			return true
		}
		return ind >= mark+2
	}
	start := cur
	for start-1 >= 0 && isRow(lines[start-1]) {
		start--
	}
	end := cur
	for end+1 < len(lines) {
		next := lines[end+1]
		if isRow(next) {
			end++
			continue
		}
		// A rule may part the last numbered rows (AskUserQuestion's "Chat about this").
		if ruleLine.MatchString(next) && end+2 < len(lines) && isRow(lines[end+2]) && numbered.MatchString(strings.TrimSpace(stripMark(lines[end+2]))) {
			end += 2
			continue
		}
		break
	}
	footer := ""
	for i, blanks := end+1, 0; i < len(lines) && blanks <= 2; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			blanks++
			continue
		}
		if footerHint.MatchString(t) {
			footer = t
		}
		break
	}

	m := &Menu{Footer: footer, Cursor: -1}
	m.Cancel = regexp.MustCompile(`(?i)\besc\b`).MatchString(footer)
	isNumbered := false
	for i := start; i <= end; i++ {
		if numbered.MatchString(strings.TrimSpace(stripMark(lines[i]))) && indent(lines[i]) <= mark+3 {
			isNumbered = true
		}
	}
	textCol := -1 // the column unnumbered rows start at
	if !isNumbered {
		textCol = indent(stripMark(lines[cur]))
	}
	// Lines above the first row belong to the text, such as the answers a review lists.
	for start < cur {
		t := strings.TrimSpace(stripMark(lines[start]))
		if isNumbered && indent(lines[start]) <= mark+3 && numbered.MatchString(t) || !isNumbered && indent(stripMark(lines[start])) == textCol {
			break
		}
		start++
	}
	prevLen := 0
	for i := start; i <= end; i++ {
		l := lines[i]
		if ruleLine.MatchString(l) {
			prevLen = 0
			continue
		}
		body := stripMark(l)
		t := strings.TrimSpace(body)
		ind := indent(body)
		row := -1
		switch {
		case isNumbered && ind <= mark+3 && numbered.MatchString(t):
			mm := numbered.FindStringSubmatch(t)
			n, _ := strconv.Atoi(mm[1])
			m.Options = append(m.Options, MenuOption{Number: n, Label: strings.TrimSpace(mm[2])})
			row = len(m.Options) - 1
		case isNumbered && (strings.EqualFold(t, "Submit") || strings.EqualFold(t, "Next")):
			m.Options = append(m.Options, MenuOption{Label: t, Submit: true})
			row = len(m.Options) - 1
		case !isNumbered && ind == textCol:
			m.Options = append(m.Options, MenuOption{Label: t})
			row = len(m.Options) - 1
		case len(m.Options) > 0:
			o := &m.Options[len(m.Options)-1]
			wrapped := cols > 0 && prevLen >= cols-1
			switch {
			case wrapped && o.Description == "":
				o.Label += t
			case wrapped:
				o.Description += t
			case o.Description == "":
				o.Description = t
			default:
				o.Description += " " + t
			}
		}
		if i == cur && row >= 0 {
			m.Cursor = row
		} else if i == cur && len(m.Options) > 0 {
			m.Cursor = len(m.Options) - 1
		}
		prevLen = utf8.RuneCountInString(l)
	}
	for i := range m.Options {
		o := &m.Options[i]
		if mm := checkBox.FindStringSubmatch(o.Label); mm != nil {
			o.Box, o.Checked, o.Label = true, mm[1] != " ", strings.TrimSpace(mm[2])
			m.Multi = true
		}
		// A list in columns, such as /model: the first column is the label.
		if parts := columns.Split(o.Label, -1); len(parts) > 1 && o.Description == "" {
			o.Label = parts[0]
			var rest []string
			for _, p := range parts[1:] {
				if p = strings.TrimSpace(p); p == "✔" {
					rest = append(rest, "current")
				} else if p != "" {
					rest = append(rest, p)
				}
			}
			o.Description = strings.Join(rest, " · ")
		}
	}
	rows := 0
	for _, o := range m.Options {
		if !o.Submit {
			rows++
		}
	}
	if m.Cursor < 0 || rows < 2 || (!isNumbered && footer == "") {
		return nil
	}
	markFreeText(m)
	m.Header, m.Title, m.Body = describe(lines[:start])
	m.Kind = menuKind(m)
	m.Sig = menuSig(m)
	return m
}

// markFreeText flags the row that takes typed text: "Type something." in an AskUserQuestion, or
// the row right before "Chat about this" once the user has typed into it.
func markFreeText(m *Menu) {
	for i := range m.Options {
		l := strings.ToLower(strings.TrimSuffix(m.Options[i].Label, "."))
		if l == "type something" || l == "other" {
			m.Options[i].FreeText = true
			return
		}
	}
	for i := range m.Options {
		if strings.EqualFold(m.Options[i].Label, "Chat about this") {
			for j := i - 1; j >= 0; j-- {
				if !m.Options[j].Submit {
					m.Options[j].FreeText = true
					return
				}
			}
		}
	}
}

// describe reads the text above a menu's rows: up to the rule that opens the dialog, skipping
// blank lines. It returns the AskUserQuestion tab line, the question and the rest.
func describe(above []string) (header, title, body string) {
	var text []string
	for i := len(above) - 1; i >= 0 && len(text) < 20; i-- {
		l := above[i]
		if ruleLine.MatchString(l) {
			break
		}
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "⏺") || strings.HasPrefix(t, "⎿") {
			break
		}
		if t == "" || dashedLine.MatchString(l) || strings.HasPrefix(t, "Tip:") {
			if len(text) > 0 && text[0] != "" {
				text = append([]string{""}, text...)
			}
			continue
		}
		if headerTabs.MatchString(t) && header == "" && (strings.ContainsAny(t, "☐☒") || strings.Contains(t, "←")) {
			header = tabTitles(t)
			continue
		}
		text = append([]string{t}, text...)
	}
	for len(text) > 0 && text[0] == "" {
		text = text[1:]
	}
	q := -1
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == "Review your answers" {
			q = i
			break
		}
	}
	for i := len(text) - 1; i >= 0 && q < 0; i-- {
		if strings.HasSuffix(text[i], "?") {
			q = i
			break
		}
	}
	if q < 0 {
		q = 0
	}
	if len(text) == 0 {
		return header, "", ""
	}
	title = text[q]
	rest := append(append([]string{}, text[:q]...), text[q+1:]...)
	body = strings.TrimSpace(strings.Join(rest, "\n"))
	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}
	return header, title, body
}

// tabTitles reads an AskUserQuestion tab line ("←  ☐ Checks  ✔ Submit  →") as its question
// titles ("Checks"), leaving out the boxes, which change as rows are ticked, and the Submit tab.
func tabTitles(t string) string {
	var out []string
	for _, p := range regexp.MustCompile(`[☐☒✔←→]`).Split(t, -1) {
		if p = strings.TrimSpace(p); p != "" && p != "Submit" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// menuKind names what a menu asks.
func menuKind(m *Menu) string {
	all := strings.ToLower(m.Title + "\n" + m.Body)
	labels := ""
	for _, o := range m.Options {
		labels += strings.ToLower(o.Label) + "\n"
	}
	switch {
	case strings.Contains(all, "mcp server"):
		return "mcp"
	case strings.Contains(all, "trust the files") || strings.Contains(all, "trust this folder"):
		return "trust"
	case strings.Contains(m.Title, "Review your answers"):
		return "review"
	case strings.Contains(all, "would you like to proceed") || strings.Contains(labels, "keep planning"):
		return "plan"
	case strings.Contains(all, "do you want to") || strings.Contains(labels, "tell claude what to do differently"):
		return "permission"
	case m.Header != "" || strings.Contains(labels, "chat about this"):
		return "question"
	}
	return "select"
}

// menuSig hashes what makes a menu the same menu while it is up: its kind, text and the rows'
// labels. The free-text row is left out, since it shows what is being typed.
func menuSig(m *Menu) string {
	h := sha1.New()
	h.Write([]byte(m.Kind + "\x00" + m.Header + "\x00" + m.Title + "\x00" + m.Body))
	for _, o := range m.Options {
		if !o.FreeText {
			h.Write([]byte("\x00" + o.Label))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// unbox strips a dialog box's side borders and trailing blanks.
func unbox(l string) string {
	l = strings.TrimRight(l, " \t")
	if strings.HasPrefix(strings.TrimLeft(l, " "), "│") {
		i := strings.Index(l, "│")
		l = strings.Repeat(" ", utf8.RuneCountInString(l[:i])) + " " + l[i+len("│"):]
		l = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(l, " "), "│"), " ")
	}
	return l
}

// stripMark replaces the cursor arrow with a blank, so a highlighted row reads like the others.
func stripMark(l string) string { return strings.Replace(l, string(cursorMark), " ", 1) }

// indent is the column of a line's first non-blank character.
func indent(l string) int {
	n := 0
	for _, r := range l {
		if r != ' ' {
			return n
		}
		n++
	}
	return n
}

// runeIndex is the column of r in l, or -1.
func runeIndex(l string, r rune) int {
	n := 0
	for _, c := range l {
		if c == r {
			return n
		}
		n++
	}
	return -1
}

// byteAt is the byte offset of column col in l.
func byteAt(l string, col int) int {
	n := 0
	for i := range l {
		if n == col {
			return i
		}
		n++
	}
	return len(l)
}
