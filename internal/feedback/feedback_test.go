package feedback

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTokens struct{ tok string }

func (f *fakeTokens) Get(context.Context, string) string { return f.tok }
func (f *fakeTokens) Set(_ context.Context, _, t string) error {
	f.tok = t
	return nil
}
func (f *fakeTokens) Delete(context.Context, string) error {
	f.tok = ""
	return nil
}

var mac = Details{Home: "/Users/jdoe", User: "jdoe", Host: "Jdoes-MacBook.local"}

func TestScrubHidesPathsUserAndHost(t *testing.T) {
	in := "Opening /Users/jdoe/Documents/acme/app fails on Jdoes-MacBook, user jdoe. See ~/Library/Logs/x.log and /private/var/folders/ab/T/y. jdoeish stays."
	got, removed := Scrub(in, mac)
	for _, leak := range []string{"/Users/jdoe", "acme", "Jdoes-MacBook", " jdoe.", "Library", "/private"} {
		if strings.Contains(got, leak) {
			t.Errorf("%q left in %q", leak, got)
		}
	}
	if !strings.Contains(got, "jdoeish stays") || !strings.Contains(got, "<path>") || !strings.Contains(got, "<host>") || !strings.Contains(got, "user <user>.") {
		t.Errorf("scrubbed = %q", got)
	}
	if strings.Join(removed, ",") != "paths,host name,user name" {
		t.Errorf("removed = %v", removed)
	}
	if s, _ := Scrub("It was /Users/jdoe/x/billing-fix.", mac); s != "It was <path>." {
		t.Errorf("the full stop stays: %q", s)
	}
	if s, r := Scrub("Nothing to hide here", mac); s != "Nothing to hide here" || len(r) != 0 {
		t.Errorf("clean text changed: %q %v", s, r)
	}
}

func TestMakeDraft(t *testing.T) {
	n := Note{ID: "n1", Kind: "bug", Text: "The board at /Users/jdoe/x flickers", Area: "board", Version: "v0.2.32", MacOS: "15.1"}
	d := MakeDraft(n, mac, false)
	if d.Title != "[bug] The board at <path> flickers" {
		t.Errorf("title = %q", d.Title)
	}
	for _, want := range []string{"Kind: bug", "Area: board", "Hadron version: v0.2.32", "macOS: 15.1"} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, d.Body)
		}
	}
	if strings.Join(d.Labels, ",") != "feedback,bug" || strings.Join(d.Removed, ",") != "paths" {
		t.Errorf("labels %v removed %v", d.Labels, d.Removed)
	}
	if k := MakeDraft(n, mac, true); !strings.Contains(k.Body, "/Users/jdoe/x") || len(k.Removed) != 0 {
		t.Errorf("keep = %+v", k)
	}
	long := MakeDraft(Note{Kind: "idea", Text: strings.Repeat("word ", 40), Version: "v1"}, Details{}, false)
	if r := []rune(long.Title); len(r) > 80 || !strings.HasSuffix(long.Title, "…") {
		t.Errorf("long title = %q", long.Title)
	}
}

func TestPrivateHitsAndCleanWords(t *testing.T) {
	words := CleanWords([]string{" Acme ", "", "acme", "Beta Corp", "zeta"})
	if strings.Join(words, "|") != "Acme|Beta Corp|zeta" {
		t.Fatalf("clean = %v", words)
	}
	if got := PrivateHits(words, "Seen at ACME today", "with beta corp"); strings.Join(got, ",") != "Acme,Beta Corp" {
		t.Errorf("hits = %v", got)
	}
	if got := PrivateHits(words, "nothing"); len(got) != 0 {
		t.Errorf("hits = %v", got)
	}
}

func TestNewIssueURLFits(t *testing.T) {
	s := &Service{}
	u := s.NewIssueURL("[idea] x", strings.Repeat("a long wish ", 2000), []string{"feedback", "idea"})
	if len(u) > URLLimit {
		t.Fatalf("url is %d bytes", len(u))
	}
	p, _ := url.Parse(u)
	if p.Host != "github.com" || p.Path != "/GabiHert/hadron/issues/new" || p.Query().Get("labels") != "feedback,idea" || !strings.HasSuffix(p.Query().Get("body"), "…") {
		t.Errorf("url = %s", u)
	}
}

// fakeGitHub serves the issues API: POST creates, GET lists what was created plus fixed ones.
type fakeGitHub struct {
	srv     *httptest.Server
	gets    atomic.Int32
	posts   []map[string]any
	auth    string
	fail    bool
	created []map[string]any
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/GabiHert/hadron/issues" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			f.auth = r.Header.Get("Authorization")
			var in map[string]any
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &in)
			f.posts = append(f.posts, in)
			n := 100 + len(f.posts)
			var labels []map[string]string
			for _, l := range in["labels"].([]any) {
				labels = append(labels, map[string]string{"name": l.(string)})
			}
			f.created = append(f.created, map[string]any{"number": n, "title": in["title"], "body": in["body"], "html_url": "https://github.com/GabiHert/hadron/issues/" + itoa(n), "labels": labels, "created_at": "2026-11-17T10:00:00Z"})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "https://github.com/GabiHert/hadron/issues/" + itoa(n)})
			return
		}
		f.gets.Add(1)
		if f.fail {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			return
		}
		list := []map[string]any{
			{"number": 1, "title": "[idea] Pin cards", "html_url": "https://github.com/GabiHert/hadron/issues/1", "body": "Let me pin a card\n\n---\nKind: idea\nArea: board\nHadron version: v0.2.30\n", "labels": []map[string]string{{"name": "feedback"}, {"name": "idea"}}, "created_at": "2026-11-10T10:00:00Z"},
			{"number": 2, "title": "Unrelated bug", "html_url": "https://github.com/GabiHert/hadron/issues/2", "body": "not feedback", "labels": []map[string]string{{"name": "bug"}}},
			{"number": 3, "title": "A pull request", "html_url": "https://github.com/GabiHert/hadron/pull/3", "body": "", "labels": []map[string]string{{"name": "feedback"}}, "pull_request": map[string]any{}},
			{"number": 4, "title": "[bug] Sent from the browser", "html_url": "https://github.com/GabiHert/hadron/issues/4", "body": "Flicker\n\n---\nKind: bug\nHadron version: v0.2.31\n<!-- hadron-feedback:note-4 -->\n", "labels": []map[string]string{}},
		}
		list = append(list, f.created...)
		_ = json.NewEncoder(w).Encode(list)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestSendWithTokenAndThroughTheBrowser(t *testing.T) {
	gh := newFakeGitHub(t)
	tok := &fakeTokens{}
	var opened string
	s := &Service{APIBase: gh.srv.URL, Tokens: tok, Open: func(u string) error { opened = u; return nil }}
	ctx := context.Background()

	sent, err := s.Send(ctx, "[bug] x", "body", []string{"feedback", "bug"})
	if err != nil || sent.Via != "browser" || opened != sent.URL || !strings.Contains(sent.URL, "/GabiHert/hadron/issues/new?") || len(gh.posts) != 0 {
		t.Fatalf("browser send = %+v %v (posts %d)", sent, err, len(gh.posts))
	}

	if err := s.SetToken(ctx, "github_pat_abcdefghijklmnopqrstuvwxyz"); err != nil || !s.HasToken(ctx) {
		t.Fatal("token not saved")
	}
	sent, err = s.Send(ctx, "[bug] x", "body", []string{"feedback", "bug"})
	if err != nil || sent.Via != "token" || sent.URL != "https://github.com/GabiHert/hadron/issues/101" {
		t.Fatalf("token send = %+v %v", sent, err)
	}
	if gh.auth != "Bearer github_pat_abcdefghijklmnopqrstuvwxyz" || gh.posts[0]["title"] != "[bug] x" {
		t.Errorf("post = %v auth %q", gh.posts[0], gh.auth)
	}
	_ = s.SetToken(ctx, "")
	if s.HasToken(ctx) {
		t.Error("an empty token removes it")
	}
}

func TestListFiltersCachesAndFallsBack(t *testing.T) {
	gh := newFakeGitHub(t)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s := &Service{APIBase: gh.srv.URL, CachePath: filepath.Join(t.TempDir(), "shared.json"), Now: func() time.Time { return now }}
	ctx := context.Background()

	sh := s.List(ctx, false)
	if sh.Error != "" || len(sh.Issues) != 2 {
		t.Fatalf("list = %+v", sh)
	}
	a, b := sh.Issues[0], sh.Issues[1]
	if a.Number != 1 || a.Kind != "idea" || a.Version != "v0.2.30" || a.Area != "board" || a.Text != "Let me pin a card" {
		t.Errorf("labelled issue = %+v", a)
	}
	if b.Number != 4 || b.NoteID != "note-4" || b.Kind != "bug" || b.Text != "Flicker" {
		t.Errorf("marked issue = %+v", b)
	}

	s.List(ctx, false)
	if gh.gets.Load() != 1 {
		t.Errorf("a fresh cache is used: %d fetches", gh.gets.Load())
	}
	s.List(ctx, true)
	now = now.Add(CacheTTL + time.Minute)
	s.List(ctx, false)
	if gh.gets.Load() != 3 {
		t.Errorf("refresh and a stale cache fetch: %d fetches", gh.gets.Load())
	}

	gh.fail = true
	sh = s.List(ctx, true)
	if !strings.Contains(sh.Error, "limit") || len(sh.Issues) != 2 {
		t.Errorf("a failed fetch keeps the cache: %+v", sh)
	}
	fresh := &Service{APIBase: gh.srv.URL, CachePath: filepath.Join(t.TempDir(), "none.json")}
	if sh := fresh.List(ctx, false); sh.Error == "" || sh.Issues == nil || len(sh.Issues) != 0 {
		t.Errorf("no cache and no network = %+v", sh)
	}
}
