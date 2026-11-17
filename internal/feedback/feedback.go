// Package feedback shares feedback notes as public GitHub issues and reads everyone's back.
//
// Sending is never automatic: the caller builds a draft (Draft), the user reviews and edits it,
// and only then is it sent, with a saved token through the API or, with none, as a filled-in
// new-issue page in the browser. Reading uses the public API with no token and keeps a cache in the
// data folder, so the "All users" list works offline and stays under the unauthenticated limit.
package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// Repo is where shared feedback goes.
	Repo = "GabiHert/hadron"
	// Label marks shared feedback; the kind is a second label.
	Label = "feedback"
	// CacheTTL is how long the shared list is used before it is fetched again.
	CacheTTL = 3 * time.Hour
	// URLLimit keeps a prefilled new-issue link under what GitHub accepts.
	URLLimit = 7000
	apiBase  = "https://api.github.com"
	webBase  = "https://github.com"
	// markerPrefix starts the hidden line that ties an issue to the note it came from, so a note
	// sent through the browser gets its issue link once the shared list sees it.
	markerPrefix = "<!-- hadron-feedback:"
)

// Tokens keeps the sending token. update.Keychain fits; tests use a fake.
type Tokens interface {
	Get(ctx context.Context, repo string) string
	Set(ctx context.Context, repo, token string) error
	Delete(ctx context.Context, repo string) error
}

// Issue is one shared note, as the "All users" list shows it.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Kind      string    `json:"kind" jsonschema:"bug, idea, annoyance or praise; empty when the issue has no kind label"`
	Version   string    `json:"version" jsonschema:"the Hadron version it was sent from, when the body says"`
	Area      string    `json:"area"`
	Text      string    `json:"text" jsonschema:"the note's text, without the details footer"`
	NoteID    string    `json:"note_id,omitempty" jsonschema:"the id of the note it was sent from, when it carries the marker"`
	CreatedAt time.Time `json:"created_at"`
}

// Service talks to GitHub. The zero value is not usable; fill Tokens and CachePath.
type Service struct {
	Repo      string // owner/name; Repo when empty
	APIBase   string // tests point it at an httptest server
	WebBase   string
	Client    *http.Client
	Tokens    Tokens
	CachePath string
	// Open shows a URL in the browser. It may be nil, as in tests: the URL is then only returned.
	Open func(url string) error
	Now  func() time.Time
	// Local returns what a draft hides; LocalDetails when nil. Tests set it.
	Local func() Details

	mu sync.Mutex
}

// Details returns this Mac's details a draft hides.
func (s *Service) Details() Details {
	if s.Local != nil {
		return s.Local()
	}
	return LocalDetails()
}

func (s *Service) repo() string { return or(s.Repo, Repo) }
func (s *Service) api() string  { return strings.TrimRight(or(s.APIBase, apiBase), "/") }
func (s *Service) web() string  { return strings.TrimRight(or(s.WebBase, webBase), "/") }
func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// HasToken says whether a sending token is saved.
func (s *Service) HasToken(ctx context.Context) bool {
	return s.Tokens != nil && s.Tokens.Get(ctx, s.repo()) != ""
}

// SetToken saves the sending token; an empty one removes it.
func (s *Service) SetToken(ctx context.Context, token string) error {
	if s.Tokens == nil {
		return errors.New("this build cannot keep a token")
	}
	if strings.TrimSpace(token) == "" {
		return s.Tokens.Delete(ctx, s.repo())
	}
	return s.Tokens.Set(ctx, s.repo(), token)
}

// Note is what a draft is made from.
type Note struct {
	ID, Kind, Text, Area, Version, MacOS string
}

// Details are what a draft hides unless the user keeps them.
type Details struct {
	Home, User, Host string
}

// LocalDetails reads this Mac's home folder, user name and host name.
func LocalDetails() Details {
	d := Details{}
	d.Home, _ = os.UserHomeDir()
	d.User = os.Getenv("USER")
	if d.User == "" && d.Home != "" {
		d.User = filepath.Base(d.Home)
	}
	d.Host, _ = os.Hostname()
	return d
}

// Draft is the issue as it would be sent, for the user to review and edit.
type Draft struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
	// Removed says what was taken out: "paths", "user name", "host name".
	Removed []string `json:"removed"`
}

// MakeDraft builds the issue for a note. Unless keep is set, folder paths, the user name and the
// host name are replaced, and Removed says which.
func MakeDraft(n Note, d Details, keep bool) Draft {
	text := strings.TrimSpace(n.Text)
	area := n.Area
	var removed []string
	if !keep {
		var r []string
		text, r = Scrub(text, d)
		removed = append(removed, r...)
		area, r = Scrub(area, d)
		for _, x := range r {
			if !containsStr(removed, x) {
				removed = append(removed, x)
			}
		}
	}
	first := strings.Join(strings.Fields(text), " ")
	if r := []rune(first); len(r) > 70 {
		first = string(r[:69]) + "…"
	}
	var b strings.Builder
	b.WriteString(text)
	b.WriteString("\n\n---\n")
	fmt.Fprintf(&b, "Kind: %s\n", n.Kind)
	if area != "" {
		fmt.Fprintf(&b, "Area: %s\n", area)
	}
	fmt.Fprintf(&b, "Hadron version: %s\n", n.Version)
	if n.MacOS != "" {
		fmt.Fprintf(&b, "macOS: %s\n", n.MacOS)
	}
	return Draft{Title: fmt.Sprintf("[%s] %s", n.Kind, first), Body: b.String(), Labels: Labels(n.Kind), Removed: removed}
}

// Labels are the labels a note of a kind gets.
func Labels(kind string) []string {
	if kind == "" {
		return []string{Label}
	}
	return []string{Label, kind}
}

// pathRE matches folder paths that would tell where things live on this Mac.
var pathRE = regexp.MustCompile("(?:~|/(?:Users|private|var|Volumes|tmp|opt|home))(?:/[^\\s\"'`<>|,;:()\\[\\]]+)+/?")

// Scrub replaces folder paths, the user name and the host name in s, and says which it found.
func Scrub(s string, d Details) (string, []string) {
	var found []string
	add := func(x string) {
		if !containsStr(found, x) {
			found = append(found, x)
		}
	}
	if d.Home != "" && strings.Contains(s, d.Home) {
		s = strings.ReplaceAll(s, d.Home, "~")
	}
	if pathRE.MatchString(s) {
		// A full stop or other punctuation that ends the sentence stays after the placeholder.
		s = pathRE.ReplaceAllStringFunc(s, func(m string) string {
			t := strings.TrimRight(m, ".!?")
			return "<path>" + m[len(t):]
		})
		add("paths")
	}
	for _, h := range hostNames(d.Host) {
		if re := wordRE(h); re.MatchString(s) {
			s = re.ReplaceAllString(s, "${1}<host>${2}")
			add("host name")
		}
	}
	if len(d.User) >= 3 {
		if re := wordRE(d.User); re.MatchString(s) {
			s = re.ReplaceAllString(s, "${1}<user>${2}")
			add("user name")
		}
	}
	return s, found
}

// hostNames is the full host name and its first label (MacBook-Pro.local, MacBook-Pro).
func hostNames(h string) []string {
	if len(h) < 3 {
		return nil
	}
	out := []string{h}
	if short, _, ok := strings.Cut(h, "."); ok && len(short) >= 3 {
		out = append(out, short)
	}
	return out
}

func wordRE(w string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[^\pL\pN_-])` + regexp.QuoteMeta(w) + `($|[^\pL\pN_-])`)
}

// PrivateHits lists the private words that appear in the text, ignoring case, in list order.
func PrivateHits(words []string, text ...string) []string {
	all := strings.ToLower(strings.Join(text, "\n"))
	var out []string
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w != "" && strings.Contains(all, strings.ToLower(w)) && !containsStr(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// CleanWords trims the private words, drops blanks and repeats (ignoring case), and keeps the order.
func CleanWords(words []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" || seen[strings.ToLower(w)] {
			continue
		}
		seen[strings.ToLower(w)] = true
		out = append(out, w)
	}
	return out
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// WithMarker adds the hidden line that ties the issue to its note.
func WithMarker(body, noteID string) string {
	if noteID == "" || strings.Contains(body, markerPrefix) {
		return body
	}
	return strings.TrimRight(body, "\n") + "\n" + markerPrefix + noteID + " -->\n"
}

// NewIssueURL is the browser link that opens a new issue with the title, body and labels filled
// in, the body trimmed until the link fits.
func (s *Service) NewIssueURL(title, body string, labels []string) string {
	build := func(b string) string {
		q := url.Values{}
		q.Set("title", title)
		q.Set("body", b)
		if len(labels) > 0 {
			q.Set("labels", strings.Join(labels, ","))
		}
		return s.web() + "/" + s.repo() + "/issues/new?" + q.Encode()
	}
	u := build(body)
	for len(u) > URLLimit {
		r := []rune(body)
		body = string(r[:len(r)*3/4]) + "…"
		u = build(body)
	}
	return u
}

// Sent is the result of sending.
type Sent struct {
	// Via is "token" (the issue was created) or "browser" (a filled-in page was opened).
	Via string `json:"via"`
	// URL is the new issue, or the new-issue page that was opened.
	URL string `json:"url"`
}

// Send creates the issue with the saved token, or opens the new-issue page when there is none.
func (s *Service) Send(ctx context.Context, title, body string, labels []string) (Sent, error) {
	tok := ""
	if s.Tokens != nil {
		tok = s.Tokens.Get(ctx, s.repo())
	}
	if tok == "" {
		u := s.NewIssueURL(title, body, labels)
		if s.Open != nil {
			if err := s.Open(u); err != nil {
				return Sent{}, fmt.Errorf("could not open the browser: %v", err)
			}
		}
		return Sent{Via: "browser", URL: u}, nil
	}
	payload, _ := json.Marshal(map[string]any{"title": title, "body": body, "labels": labels})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.api()+"/repos/"+s.repo()+"/issues", bytes.NewReader(payload))
	if err != nil {
		return Sent{}, err
	}
	header(req)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		return Sent{}, fmt.Errorf("could not reach GitHub: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated {
		return Sent{}, apiError(resp.StatusCode, raw)
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.HTMLURL == "" {
		return Sent{}, errors.New("GitHub created the issue but did not say where")
	}
	return Sent{Via: "token", URL: out.HTMLURL}, nil
}

func apiError(code int, raw []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	switch code {
	case http.StatusUnauthorized:
		return errors.New("GitHub refused the saved token. Save a new one in Settings, Feedback, or remove it to send through the browser")
	case http.StatusForbidden, http.StatusNotFound:
		return fmt.Errorf("the saved token cannot create issues on %s. It needs Issues: write on that repo (GitHub said: %s)", Repo, or(e.Message, http.StatusText(code)))
	}
	return fmt.Errorf("GitHub answered %d: %s", code, or(e.Message, http.StatusText(code)))
}

func header(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "Hadron")
}

// Shared is the "All users" list.
type Shared struct {
	Issues    []Issue   `json:"issues"`
	FetchedAt time.Time `json:"fetched_at"`
	// Error says why the last fetch failed; the list is then the cached one.
	Error string `json:"error,omitempty"`
}

// List returns the open shared notes: the cache while it is fresh, else a fetch. refresh fetches
// anyway. A failed fetch returns the cache with the error.
func (s *Service) List(ctx context.Context, refresh bool) Shared {
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok := s.readCache()
	if ok && !refresh && s.now().Sub(cached.FetchedAt) < CacheTTL {
		return cached
	}
	issues, err := s.fetch(ctx)
	if err != nil {
		cached.Error = err.Error()
		if cached.Issues == nil {
			cached.Issues = []Issue{}
		}
		return cached
	}
	out := Shared{Issues: issues, FetchedAt: s.now().UTC()}
	s.writeCache(out)
	return out
}

func (s *Service) readCache() (Shared, bool) {
	var c Shared
	if s.CachePath == "" {
		return c, false
	}
	raw, err := os.ReadFile(s.CachePath)
	if err != nil || json.Unmarshal(raw, &c) != nil {
		return Shared{}, false
	}
	return c, true
}

func (s *Service) writeCache(c Shared) {
	if s.CachePath == "" {
		return
	}
	raw, _ := json.Marshal(c)
	tmp := s.CachePath + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, s.CachePath)
	}
}

type ghIssue struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
	PullRequest any       `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// fetch reads the open issues that are shared feedback: those labelled feedback, and those that
// carry the marker (a note sent through the browser by someone who cannot set labels).
func (s *Service) fetch(ctx context.Context) ([]Issue, error) {
	out := []Issue{}
	for page := 1; page <= 5; page++ {
		u := fmt.Sprintf("%s/repos/%s/issues?state=open&per_page=100&page=%d", s.api(), s.repo(), page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		header(req)
		resp, err := s.client().Do(req)
		if err != nil {
			return nil, fmt.Errorf("could not reach GitHub: %v", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return nil, errors.New("GitHub's limit for reading without a token was reached; the list refreshes later")
		}
		if resp.StatusCode != http.StatusOK {
			return nil, apiError(resp.StatusCode, raw)
		}
		var list []ghIssue
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, fmt.Errorf("GitHub sent a list Hadron cannot read: %v", err)
		}
		for _, g := range list {
			if g.PullRequest != nil {
				continue
			}
			if is, ok := toIssue(g); ok {
				out = append(out, is)
			}
		}
		if len(list) < 100 {
			break
		}
	}
	return out, nil
}

var (
	versionRE = regexp.MustCompile(`(?m)^Hadron version:\s*(\S+)`)
	areaRE    = regexp.MustCompile(`(?m)^Area:\s*(.+)$`)
	kindRE    = regexp.MustCompile(`(?m)^Kind:\s*(\w+)`)
	markerRE  = regexp.MustCompile(`<!-- hadron-feedback:([A-Za-z0-9-]+) -->`)
	kinds     = []string{"bug", "idea", "annoyance", "praise"}
)

func toIssue(g ghIssue) (Issue, bool) {
	labelled := false
	kind := ""
	for _, l := range g.Labels {
		if strings.EqualFold(l.Name, Label) {
			labelled = true
		}
		for _, k := range kinds {
			if strings.EqualFold(l.Name, k) {
				kind = k
			}
		}
	}
	marker := markerRE.FindStringSubmatch(g.Body)
	if !labelled && marker == nil {
		return Issue{}, false
	}
	is := Issue{Number: g.Number, Title: g.Title, URL: g.HTMLURL, Kind: kind, CreatedAt: g.CreatedAt}
	if marker != nil {
		is.NoteID = marker[1]
	}
	if is.Kind == "" {
		if m := kindRE.FindStringSubmatch(g.Body); m != nil && containsStr(kinds, strings.ToLower(m[1])) {
			is.Kind = strings.ToLower(m[1])
		}
	}
	if m := versionRE.FindStringSubmatch(g.Body); m != nil {
		is.Version = m[1]
	}
	if m := areaRE.FindStringSubmatch(g.Body); m != nil {
		is.Area = strings.TrimSpace(m[1])
	}
	text := g.Body
	if i := strings.LastIndex(text, "\n---\n"); i >= 0 {
		text = text[:i]
	}
	is.Text = strings.TrimSpace(markerRE.ReplaceAllString(text, ""))
	return is, true
}
