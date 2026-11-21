package sessions

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GabiHert/hadron/internal/pty"
)

// Menu watching and answering. The app scans every running card session's screen (ScanMenus); a
// menu that stays up for MenuStableScans scans becomes a question on the card, and one that stays
// gone closes it. Answering presses the keys (AnswerMenu) and checks that the menu closed.

// MenuStableScans is how many scans in a row must agree before a menu counts as shown or gone, so
// a half-drawn frame never makes a question.
const MenuStableScans = 2

// DefaultMenuHold is Config.MenuHold's default.
const DefaultMenuHold = 3 * time.Second

// ErrNoMenu says the card's terminal shows no menu.
var ErrNoMenu = errors.New("no menu is open")

type menuTrack struct {
	sig    string // what the last scan saw ("" for no menu)
	n      int    // how many scans in a row saw it
	stable *Menu  // the menu last seen for MenuStableScans scans; nil once it was gone as long
	known  bool   // stable has been decided at least once
}

type menuState struct {
	mu      sync.Mutex
	tracks  map[string]*menuTrack // session id ->
	driving map[string]time.Time  // session id -> keys are being pressed, or until when to hold off
}

func newMenuState() menuState {
	return menuState{tracks: map[string]*menuTrack{}, driving: map[string]time.Time{}}
}

// MenuSighting is what the watcher knows of one running card session.
type MenuSighting struct {
	WorkspaceID string
	CardID      string
	SessionID   string
	Nick        string
	Cwd         string
	// Menu is the menu that has stayed up; nil when none has. Only meaningful when Settled.
	Menu *Menu
	// Settled is false while the scans disagree, or while Hadron itself answers the menu: leave
	// the card's questions alone then.
	Settled bool
}

// ScanMenus reads every running card session's screen once and reports what each shows.
func (s *Service) ScanMenus() []MenuSighting {
	var out []MenuSighting
	live := map[string]bool{}
	now := time.Now()
	for _, row := range s.LiveSessions() {
		if row.CardID == "" || row.Kind != "card" || !s.isLatest(row, row.CardID) {
			continue
		}
		ps, ok := s.pty.Get(row.ID)
		if !ok {
			continue
		}
		live[row.ID] = true
		cols, _ := ps.Size()
		m := ParseMenu(ps.Screen(0, false), cols)
		sig := ""
		if m != nil {
			sig = m.Sig
		}
		nick := ""
		if c, err := s.cfg.Store.Cards.Get(row.CardID); err == nil {
			nick = c.Nick
		}
		sg := MenuSighting{WorkspaceID: row.WorkspaceID, CardID: row.CardID, SessionID: row.ID, Nick: nick, Cwd: row.Cwd}
		s.menus.mu.Lock()
		t := s.menus.tracks[row.ID]
		if t == nil {
			t = &menuTrack{}
			s.menus.tracks[row.ID] = t
		}
		if t.sig == sig && t.n > 0 {
			t.n++
		} else {
			t.sig, t.n = sig, 1
		}
		if t.n >= MenuStableScans {
			t.stable, t.known = m, true
		}
		until, driving := s.menus.driving[row.ID]
		if driving && !until.IsZero() && now.After(until) {
			delete(s.menus.driving, row.ID)
			driving = false
		}
		sg.Menu, sg.Settled = t.stable, t.known && t.n >= MenuStableScans && !driving
		s.menus.mu.Unlock()
		out = append(out, sg)
	}
	s.menus.mu.Lock()
	for id := range s.menus.tracks {
		if !live[id] {
			delete(s.menus.tracks, id)
		}
	}
	s.menus.mu.Unlock()
	return out
}

// CardMenu returns the menu a card's running session shows now, or ErrNoMenu.
func (s *Service) CardMenu(workspaceID, cardID string) (*Menu, error) {
	ps, err := s.cardLive(workspaceID, cardID)
	if err != nil {
		return nil, err
	}
	cols, _ := ps.Size()
	if m := ParseMenu(ps.Screen(0, false), cols); m != nil {
		return m, nil
	}
	return nil, ErrNoMenu
}

func (s *Service) cardLive(workspaceID, cardID string) (*pty.Session, error) {
	t, err := s.Find(s.cfg.Store, workspaceID, cardID)
	if err != nil {
		return nil, err
	}
	if t.Live == nil {
		return nil, errors.New("its session is not running")
	}
	return t.Live, nil
}

// MenuChoice is an answer to a menu.
type MenuChoice struct {
	// Sig, when set, is the menu the answer was given for: a different menu on screen is refused.
	Sig string
	// Labels are the rows to choose, by label or number. Several only when the menu takes several.
	Labels []string
	// Text is typed into the menu's free-text row, or, when it has none or another row was
	// chosen, sent to Claude as a message once the menu has closed.
	Text string
	// Cancel presses Esc instead.
	Cancel bool
}

// MenuResult says what answering did.
type MenuResult struct {
	// Keys are the keys pressed, in order.
	Keys []string `json:"keys"`
	// Next is the menu that showed after this one closed, such as an AskUserQuestion's next question.
	Next *Menu `json:"next,omitempty"`
	// TextSent is true when Text went to Claude as a message after the menu closed.
	TextSent bool `json:"text_sent,omitempty"`
}

// Timing of the key driver; tests shorten them.
var (
	menuKeyWait   = 700 * time.Millisecond // for the screen to show a key's effect
	menuCloseWait = 4 * time.Second        // for the menu to close after the last key
	menuSettle    = 400 * time.Millisecond // for the next menu to show once one has closed
	menuPoll      = 30 * time.Millisecond
)

// AnswerMenu answers the menu on a card's terminal: it moves the highlight with the arrow keys,
// ticks rows with Space, types into the free-text row and confirms with Enter (or presses Esc to
// cancel), then waits for the menu to close. When it does not, it tries once more from what the
// screen shows, then gives up with an error.
func (s *Service) AnswerMenu(ctx context.Context, workspaceID, cardID string, ch MenuChoice) (MenuResult, error) {
	ps, err := s.cardLive(workspaceID, cardID)
	if err != nil {
		return MenuResult{}, err
	}
	sid := ""
	if t, err := s.Find(s.cfg.Store, workspaceID, cardID); err == nil {
		sid = t.Row.ID
	}
	s.menus.mu.Lock()
	if until, busy := s.menus.driving[sid]; busy && until.IsZero() {
		s.menus.mu.Unlock()
		return MenuResult{}, errors.New("another answer to this menu is being typed")
	}
	s.menus.driving[sid] = time.Time{}
	s.menus.mu.Unlock()
	defer func() {
		s.menus.mu.Lock()
		s.menus.driving[sid] = time.Now().Add(s.cfg.MenuHold)
		s.menus.mu.Unlock()
	}()
	d := &driver{ps: ps}
	res, err := d.answer(ctx, ch)
	res.Keys = d.keys
	return res, err
}

type driver struct {
	ps   *pty.Session
	keys []string
}

func (d *driver) read() *Menu {
	cols, _ := d.ps.Size()
	return ParseMenu(d.ps.Screen(0, false), cols)
}

func (d *driver) press(k string) error {
	d.keys = append(d.keys, k)
	return d.ps.Keys([]string{k})
}

func (d *driver) typeText(t string) error {
	d.keys = append(d.keys, "text:"+t)
	return d.ps.Send(t, false)
}

// await polls the screen until ok holds or the wait ends, and returns the last menu read.
func (d *driver) await(ctx context.Context, wait time.Duration, ok func(*Menu) bool) (*Menu, bool) {
	deadline := time.Now().Add(wait)
	for {
		m := d.read()
		if ok(m) {
			return m, true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return m, false
		}
		time.Sleep(menuPoll)
	}
}

func (d *driver) answer(ctx context.Context, ch MenuChoice) (MenuResult, error) {
	m := d.read()
	if m == nil {
		return MenuResult{}, ErrNoMenu
	}
	if ch.Sig != "" && m.Sig != ch.Sig {
		return MenuResult{}, fmt.Errorf("the menu on screen is no longer the one that was answered: it now asks %q", m.Title)
	}
	targets, cancel, err := resolve(m, ch)
	if err != nil {
		return MenuResult{}, err
	}
	sig := m.Sig
	textUsed := false
	for attempt := 1; ; attempt++ {
		if cancel {
			if !m.Cancel && !strings.Contains(strings.ToLower(m.Footer), "esc") && m.Footer != "" {
				return MenuResult{}, errors.New("this menu cannot be cancelled with Esc. Pick one of its options")
			}
			err = d.press("Escape")
		} else if m.Multi {
			textUsed, err = d.multi(ctx, m, targets, ch.Text)
		} else {
			textUsed, err = d.single(ctx, m, targets, ch.Text)
		}
		if err != nil {
			return MenuResult{}, err
		}
		next, closed := d.await(ctx, menuCloseWait, func(x *Menu) bool { return x == nil || x.Sig != sig })
		if closed && next == nil {
			// Another menu may follow, such as the next question or the review of the answers.
			next, _ = d.await(ctx, menuSettle, func(x *Menu) bool { return x != nil })
		}
		if closed {
			res := MenuResult{Next: next}
			// A multi-select AskUserQuestion asks to review the answers once more: they were given.
			if next != nil && next.Kind == "review" && m.Kind == "question" && !cancel {
				if i := find(next, "Submit answers"); i >= 0 {
					if err := d.moveTo(ctx, i); err != nil {
						return res, err
					}
					if err := d.press("Enter"); err != nil {
						return res, err
					}
					rsig := next.Sig
					next, _ = d.await(ctx, menuCloseWait, func(x *Menu) bool { return x == nil || x.Sig != rsig })
					res.Next = next
				}
			}
			if ch.Text != "" && !textUsed {
				if _, ok := d.await(ctx, menuCloseWait, func(*Menu) bool { return ScreenShowsInput(d.ps.Screen(0, false)) }); ok {
					d.keys = append(d.keys, "message:"+ch.Text)
					if err := d.ps.Send(ch.Text, true); err != nil {
						return res, err
					}
					res.TextSent = true
				}
			}
			return res, nil
		}
		if attempt >= 2 || next == nil {
			return MenuResult{}, fmt.Errorf("pressed %s, but the menu %q is still open. Look at it with session_read_screen", strings.Join(d.keys, ", "), m.Title)
		}
		m = next
	}
}

// resolve turns the chosen labels into row indexes.
func resolve(m *Menu, ch MenuChoice) ([]int, bool, error) {
	if ch.Cancel {
		return nil, true, nil
	}
	var out []int
	for _, l := range ch.Labels {
		l = strings.TrimSpace(l)
		if strings.EqualFold(l, CancelLabel) || strings.EqualFold(l, "esc") || strings.EqualFold(l, "cancel") && find(m, "Cancel") < 0 {
			return nil, true, nil
		}
		i := find(m, l)
		if i < 0 {
			if n, err := strconv.Atoi(strings.TrimSuffix(l, ".")); err == nil {
				// By the number on screen; a menu without numbers counts its rows from 1.
				for j, o := range m.Options {
					if o.Number == n || o.Number == 0 && !o.Submit && j == n-1 {
						i = j
					}
				}
			}
		}
		if i < 0 {
			return nil, false, fmt.Errorf("%q is not a row of the menu. Its rows are: %s", l, rowLabels(m))
		}
		out = append(out, i)
	}
	if len(out) > 1 && !m.Multi {
		return nil, false, fmt.Errorf("the menu takes one choice, and %d were given", len(out))
	}
	if len(out) == 0 && strings.TrimSpace(ch.Text) == "" {
		return nil, false, errors.New("choose a row, write text, or cancel")
	}
	return out, false, nil
}

// CancelLabel is the option a menu question offers for Esc.
const CancelLabel = "Cancel (Esc)"

func find(m *Menu, label string) int {
	for i, o := range m.Options {
		if strings.EqualFold(o.Label, label) {
			return i
		}
	}
	return -1
}

func freeRow(m *Menu) int {
	for i, o := range m.Options {
		if o.FreeText {
			return i
		}
	}
	return -1
}

func rowLabels(m *Menu) string {
	var l []string
	for _, o := range m.Options {
		if !o.FreeText {
			l = append(l, o.Label)
		}
	}
	if freeRow(m) >= 0 {
		l = append(l, "or text for its free-text row")
	}
	return strings.Join(l, ", ")
}

// moveTo presses Up or Down until row i is highlighted.
func (d *driver) moveTo(ctx context.Context, i int) error {
	for step := 0; step < 3*len(d.lastOptions())+6; step++ {
		m := d.read()
		if m == nil {
			return ErrNoMenu
		}
		if m.Cursor == i {
			return nil
		}
		key := "Down"
		if m.Cursor > i {
			key = "Up"
		}
		from := m.Cursor
		if err := d.press(key); err != nil {
			return err
		}
		d.await(ctx, menuKeyWait, func(x *Menu) bool { return x == nil || x.Cursor != from })
	}
	return fmt.Errorf("could not move the highlight to row %d", i+1)
}

func (d *driver) lastOptions() []MenuOption {
	if m := d.read(); m != nil {
		return m.Options
	}
	return nil
}

func (d *driver) single(ctx context.Context, m *Menu, targets []int, text string) (bool, error) {
	target, textUsed := -1, false
	if len(targets) == 1 {
		target = targets[0]
	}
	if fr := freeRow(m); text != "" && (target < 0 || target == fr) && fr >= 0 {
		target, textUsed = fr, true
	}
	if target < 0 {
		return false, errors.New("the menu has no row to type into. Choose one of its rows; the text is then sent to Claude after it")
	}
	if err := d.moveTo(ctx, target); err != nil {
		return false, err
	}
	if textUsed {
		if err := d.typeText(text); err != nil {
			return false, err
		}
		d.await(ctx, menuKeyWait, func(x *Menu) bool { return x == nil || x.Options[x.Cursor].Label != m.Options[target].Label })
	}
	return textUsed, d.press("Enter")
}

func (d *driver) multi(ctx context.Context, m *Menu, targets []int, text string) (bool, error) {
	want := map[int]bool{}
	for _, t := range targets {
		want[t] = true
	}
	fr := freeRow(m)
	textUsed := text != "" && fr >= 0
	for i, o := range m.Options {
		if !o.Box || o.FreeText {
			continue
		}
		if o.Checked == want[i] {
			continue
		}
		if err := d.moveTo(ctx, i); err != nil {
			return false, err
		}
		if err := d.press("Space"); err != nil {
			return false, err
		}
		if _, ok := d.await(ctx, menuKeyWait, func(x *Menu) bool { return x != nil && i < len(x.Options) && x.Options[i].Checked == want[i] }); !ok {
			return false, fmt.Errorf("pressed Space on %q, but its box did not change", o.Label)
		}
	}
	if textUsed {
		if err := d.moveTo(ctx, fr); err != nil {
			return false, err
		}
		if err := d.typeText(text); err != nil {
			return false, err
		}
		d.await(ctx, menuKeyWait, func(x *Menu) bool {
			return x == nil || fr >= len(x.Options) || x.Options[fr].Label != m.Options[fr].Label
		})
	}
	submit := -1
	for i, o := range m.Options {
		if o.Submit {
			submit = i
		}
	}
	if submit >= 0 {
		if err := d.moveTo(ctx, submit); err != nil {
			return false, err
		}
	}
	return textUsed, d.press("Enter")
}
