package pty

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

var namedKeys = map[string]string{
	"enter": "\r", "return": "\r", "tab": "\t", "btab": "\x1b[Z", "shift-tab": "\x1b[Z",
	"escape": "\x1b", "esc": "\x1b", "space": " ", "backspace": "\x7f", "delete": "\x1b[3~",
	"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
	"home": "\x1b[H", "end": "\x1b[F", "pageup": "\x1b[5~", "pagedown": "\x1b[6~",
}

// KeyNames lists the key names Keys accepts, besides Ctrl-<letter> and a single character.
func KeyNames() []string {
	out := make([]string, 0, len(namedKeys))
	for k := range namedKeys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// keyBytes turns key names into the bytes a terminal sends: Enter, Escape, Up, Ctrl-C, or one
// literal character such as y.
func keyBytes(keys []string) (string, error) {
	var b strings.Builder
	for _, k := range keys {
		low := strings.ToLower(strings.TrimSpace(k))
		if s, ok := namedKeys[low]; ok {
			b.WriteString(s)
			continue
		}
		if rest, ok := strings.CutPrefix(low, "ctrl-"); ok || strings.HasPrefix(low, "c-") {
			if !ok {
				rest = low[2:]
			}
			if len(rest) == 1 && rest[0] >= 'a' && rest[0] <= 'z' {
				b.WriteByte(rest[0] - 'a' + 1)
				continue
			}
		}
		if utf8.RuneCountInString(k) == 1 {
			b.WriteString(k)
			continue
		}
		return "", fmt.Errorf("unknown key %q. Use one character, Ctrl-<letter>, or one of: %s", k, strings.Join(KeyNames(), ", "))
	}
	return b.String(), nil
}
