// Command fakemenu stands in for the claude CLI in tests and demos of Hadron's menu handling. It
// draws menus the way Claude Code does (an MCP server approval, a permission prompt, an
// AskUserQuestion with one or several choices) and answers the keys a terminal sends: the arrows,
// Space, Enter, Esc and typed text. Each answer is appended to $HADRON_TEST_OUT.answers as
// "<title>|<choice>[,<choice>]|<typed text>" or "<title>|cancel".
//
// The menus come from the JSON file $HADRON_FAKE_MENUS, shown one after another at start (only the
// first $HADRON_FAKE_MENUS_START of them when that is set). After
// them it shows an input box and appends each line typed to $HADRON_TEST_OUT.in; a line
// "/menu <n>" shows menu n again (from 1), so a demo can bring a menu up on request.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Spec is one menu.
type Spec struct {
	// Style is plain (unnumbered rows, an MCP server approval), numbered (a permission prompt),
	// ask (an AskUserQuestion with one choice) or multi (with several).
	Style   string   `json:"style"`
	Header  string   `json:"header,omitempty"`
	Title   string   `json:"title"`
	Body    []string `json:"body,omitempty"`
	Options []Option `json:"options"`
	Cursor  int      `json:"cursor,omitempty"`
	Footer  string   `json:"footer,omitempty"`
	// Stubborn ignores the first Enter, to test that Hadron tries again.
	Stubborn bool `json:"stubborn,omitempty"`
}

// Option is one row.
type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Checked     bool   `json:"checked,omitempty"`
}

type row struct {
	label, desc  string
	box, checked bool
	free, submit bool
	chat         bool
	number       int
	typed        string
}

var out = os.Getenv("HADRON_TEST_OUT")

func main() {
	var specs []Spec
	if p := os.Getenv("HADRON_FAKE_MENUS"); p != "" {
		b, err := os.ReadFile(p)
		if err == nil {
			_ = json.Unmarshal(b, &specs)
		}
	}
	raw := exec.Command("stty", "raw", "-echo")
	raw.Stdin = os.Stdin
	_ = raw.Run()
	in := bufio.NewReader(os.Stdin)
	keys := make(chan string, 64)
	go readKeys(in, keys)
	first := len(specs)
	if n, err := strconv.Atoi(os.Getenv("HADRON_FAKE_MENUS_START")); err == nil && n >= 0 && n < first {
		first = n
	}
	for _, s := range specs[:first] {
		run(s, keys)
	}
	prompt(specs, keys)
}

// readKeys turns input bytes into key names: up, down, enter, esc, space, backspace, or the text.
func readKeys(in *bufio.Reader, keys chan<- string) {
	for {
		b, err := in.ReadByte()
		if err != nil {
			close(keys)
			return
		}
		switch b {
		case 0x1b:
			time.Sleep(30 * time.Millisecond)
			if in.Buffered() >= 2 {
				b1, _ := in.ReadByte()
				b2, _ := in.ReadByte()
				if b1 == '[' {
					switch b2 {
					case 'A':
						keys <- "up"
					case 'B':
						keys <- "down"
					case '2': // a bracketed paste: read to its end marker
						var sb strings.Builder
						for {
							c, err := in.ReadByte()
							if err != nil {
								break
							}
							sb.WriteByte(c)
							if strings.HasSuffix(sb.String(), "\x1b[201~") {
								break
							}
						}
						t := strings.TrimPrefix(sb.String(), "00~")
						keys <- "text:" + strings.TrimSuffix(t, "\x1b[201~")
					}
				}
				continue
			}
			keys <- "esc"
		case '\r', '\n':
			keys <- "enter"
		case ' ':
			keys <- "space"
		case 0x7f:
			keys <- "backspace"
		case 0x03:
			os.Exit(0)
		default:
			keys <- "text:" + string(b)
		}
	}
}

func rows(s Spec) []row {
	var rs []row
	for i, o := range s.Options {
		r := row{label: o.Label, desc: o.Description, checked: o.Checked}
		if s.Style == "ask" || s.Style == "multi" || s.Style == "numbered" {
			r.number = i + 1
		}
		r.box = s.Style == "multi"
		rs = append(rs, r)
	}
	switch s.Style {
	case "ask":
		rs = append(rs, row{label: "Type something.", free: true, number: len(rs) + 1})
		rs = append(rs, row{label: "Chat about this", chat: true, number: len(rs) + 1})
	case "multi":
		rs = append(rs, row{label: "Type something", free: true, box: true, number: len(rs) + 1})
		rs = append(rs, row{label: "Submit", submit: true})
		rs = append(rs, row{label: "Chat about this", chat: true, number: len(rs)})
	}
	return rs
}

func run(s Spec, keys <-chan string) {
	rs := rows(s)
	cur := s.Cursor
	stubborn := s.Stubborn
	draw(s, rs, cur)
	for k := range keys {
		switch {
		case k == "up":
			cur = (cur - 1 + len(rs)) % len(rs)
		case k == "down":
			cur = (cur + 1) % len(rs)
		case k == "esc":
			record(s.Title + "|cancel")
			clear()
			return
		case k == "space" && rs[cur].box && !rs[cur].free:
			rs[cur].checked = !rs[cur].checked
		case k == "space" && rs[cur].free:
			rs[cur].typed += " "
		case k == "backspace" && rs[cur].free && rs[cur].typed != "":
			rs[cur].typed = rs[cur].typed[:len(rs[cur].typed)-1]
		case strings.HasPrefix(k, "text:") && rs[cur].free:
			rs[cur].typed += strings.TrimPrefix(k, "text:")
			if rs[cur].box {
				rs[cur].checked = rs[cur].typed != ""
			}
		case k == "enter" && stubborn:
			stubborn = false
		case k == "enter" && s.Style == "multi" && !rs[cur].submit && !rs[cur].chat:
			rs[cur].checked = !rs[cur].checked
		case k == "enter":
			var picked []string
			typed := ""
			if s.Style == "multi" && !rs[cur].chat {
				for _, r := range rs {
					if r.checked && !r.free {
						picked = append(picked, r.label)
					}
					if r.free && r.typed != "" {
						typed = r.typed
					}
				}
				if !review(s, picked, typed, keys) {
					record(s.Title + "|cancel")
					clear()
					return
				}
			} else if rs[cur].free {
				typed = rs[cur].typed
			} else {
				picked = []string{rs[cur].label}
			}
			record(s.Title + "|" + strings.Join(picked, ",") + "|" + typed)
			clear()
			return
		}
		draw(s, rs, cur)
	}
}

// review is the last step of a multi-select AskUserQuestion: Submit answers or Cancel.
func review(s Spec, picked []string, typed string, keys <-chan string) bool {
	cur := 0
	show := func() {
		var b strings.Builder
		b.WriteString(strings.Repeat("─", 80) + "\r\n")
		b.WriteString("←  ☒ " + s.Header + "  ✔ Submit  →\r\n\r\nReview your answers\r\n\r\n")
		b.WriteString(" ● " + s.Title + "\r\n")
		ans := strings.Join(append(append([]string{}, picked...), typed), ", ")
		b.WriteString("   → " + strings.Trim(ans, ", ") + "\r\n\r\n")
		for i, l := range []string{"Submit answers", "Cancel"} {
			m := "  "
			if i == cur {
				m = "❯ "
			}
			b.WriteString(fmt.Sprintf("%s%d. %s\r\n", m, i+1, l))
		}
		paint(b.String())
	}
	show()
	for k := range keys {
		switch k {
		case "up", "down":
			cur = 1 - cur
		case "esc":
			return false
		case "enter":
			return cur == 0
		}
		show()
	}
	return false
}

func draw(s Spec, rs []row, cur int) {
	var b strings.Builder
	nl := func(l string) { b.WriteString(l + "\r\n") }
	nl(strings.Repeat("─", 80))
	pad := " "
	switch s.Style {
	case "plain":
		pad = "  "
	case "ask":
		nl(" ☐ " + s.Header)
		nl("")
		pad = ""
	case "multi":
		box := "☐"
		for _, r := range rs {
			if r.checked {
				box = "☒"
			}
		}
		nl("←  " + box + " " + s.Header + "  ✔ Submit  →")
		nl("")
		pad = ""
	}
	nl(pad + s.Title)
	nl("")
	for _, l := range s.Body {
		nl(pad + l)
	}
	if len(s.Body) > 0 {
		nl("")
	}
	for i, r := range rs {
		mark := "  "
		if i == cur {
			mark = "❯ "
		}
		if r.chat {
			nl(strings.Repeat("─", 80))
		}
		label := r.label
		if r.free && r.typed != "" {
			label = r.typed
		}
		switch {
		case s.Style == "plain":
			nl(pad + mark + label)
		case r.submit:
			nl(pad + mark + "   " + label)
		case r.box:
			c := " "
			if r.checked {
				c = "✔"
			}
			nl(pad + mark + strconv.Itoa(r.number) + ". [" + c + "] " + label)
			if r.desc != "" {
				nl(pad + "         " + r.desc)
			}
		default:
			nl(pad + mark + strconv.Itoa(r.number) + ". " + label)
			if r.desc != "" {
				nl(pad + "     " + r.desc)
			}
		}
	}
	nl("")
	footer := s.Footer
	if footer == "" {
		footer = "Enter to confirm · Esc to cancel"
	}
	nl(pad + footer)
	paint(b.String())
}

func paint(s string) { fmt.Print("\x1b[2J\x1b[H" + s) }

func clear() { paint("") }

func record(line string) {
	if out == "" {
		return
	}
	f, err := os.OpenFile(out+".answers", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// prompt is Claude's input box: it records each line typed, and "/menu <n>" shows a menu again.
func prompt(specs []Spec, keys <-chan string) {
	line := ""
	show := func() {
		paint(strings.Repeat("─", 80) + "\r\n❯ " + line + "\r\n" + strings.Repeat("─", 80) + "\r\n")
	}
	show()
	for k := range keys {
		switch {
		case k == "enter":
			if out != "" {
				if f, err := os.OpenFile(out+".in", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
					fmt.Fprintln(f, line)
					f.Close()
				}
			}
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "/menu"))); err == nil && strings.HasPrefix(line, "/menu") && n >= 1 && n <= len(specs) {
				line = ""
				run(specs[n-1], keys)
			}
			if line == "/exit" {
				return
			}
			line = ""
		case k == "space":
			line += " "
		case k == "backspace" && line != "":
			line = line[:len(line)-1]
		case strings.HasPrefix(k, "text:"):
			line += strings.TrimPrefix(k, "text:")
		}
		show()
	}
}
