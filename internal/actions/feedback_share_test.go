package actions

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/GabiHert/hadron/internal/feedback"
)

type memTokens struct {
	mu  sync.Mutex
	tok string
}

func (m *memTokens) Get(context.Context, string) string { m.mu.Lock(); defer m.mu.Unlock(); return m.tok }
func (m *memTokens) Set(_ context.Context, _, t string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tok = t
	return nil
}
func (m *memTokens) Delete(context.Context, string) error { return m.Set(nil, "", "") }

// issuesAPI is a fake GitHub issues endpoint: what is posted is created and listed back.
type issuesAPI struct {
	mu     sync.Mutex
	issues []map[string]any
	posts  int
}

func (a *issuesAPI) add(title, body string, labels ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(a.issues) + 1
	ls := []map[string]string{}
	for _, l := range labels {
		ls = append(ls, map[string]string{"name": l})
	}
	a.issues = append(a.issues, map[string]any{"number": n, "title": title, "body": body, "labels": ls,
		"html_url": "https://github.com/GabiHert/hadron/issues/" + strconv.Itoa(n), "created_at": "2026-11-17T10:00:00Z"})
}

func (a *issuesAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var in struct {
			Title, Body string
			Labels      []string
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &in)
		a.add(in.Title, in.Body, in.Labels...)
		a.mu.Lock()
		a.posts++
		n := len(a.issues)
		a.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"html_url": "https://github.com/GabiHert/hadron/issues/" + strconv.Itoa(n)})
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = json.NewEncoder(w).Encode(a.issues)
}

func shareHarness(t *testing.T) (*harness, *issuesAPI, *memTokens, *[]string) {
	t.Helper()
	deps, rec := testDeps(t)
	api := &issuesAPI{}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	tok := &memTokens{}
	opened := &[]string{}
	deps.Feedback = &feedback.Service{APIBase: srv.URL, Tokens: tok, CachePath: filepath.Join(deps.Home, "feedback-shared.json"),
		Open:  func(u string) error { *opened = append(*opened, u); return nil },
		Local: func() feedback.Details { return feedback.Details{Home: "/Users/jdoe", User: "jdoe", Host: "Jdoes-Mac.local"} }}
	r := NewRegistry(deps)
	RegisterAll(r)
	return &harness{t: t, reg: r, rec: rec}, api, tok, opened
}

func TestFeedbackSendPreviewScrubsAndBlocksPrivateWords(t *testing.T) {
	h, api, _, opened := shareHarness(t)
	var f FeedbackView
	h.do("feedback_create", m{"kind": "bug", "text": "Acme's board at /Users/jdoe/work/acme flickers on Jdoes-Mac", "area": "board"}, &f)

	var pre FeedbackSendOut
	h.do("feedback_send", m{"id": f.ID, "preview": true}, &pre)
	if strings.Contains(pre.Draft.Body, "/Users/jdoe") || strings.Contains(pre.Draft.Body, "Jdoes-Mac") || pre.Via != "browser" || pre.URL != "" {
		t.Fatalf("preview = %+v", pre)
	}
	if !strings.Contains(pre.Draft.Body, "Hadron version: test") || strings.Join(pre.Draft.Labels, ",") != "feedback,bug" || len(pre.Draft.Removed) != 2 {
		t.Errorf("draft = %+v", pre.Draft)
	}
	var keep FeedbackSendOut
	h.do("feedback_send", m{"id": f.ID, "preview": true, "keep_details": true}, &keep)
	if !strings.Contains(keep.Draft.Body, "/Users/jdoe/work/acme") {
		t.Errorf("keep_details keeps the path: %s", keep.Draft.Body)
	}

	var set FeedbackSharingOut
	h.do("feedback_sharing_set", m{"private_words": []string{" acme ", "", "ACME", "Beta"}}, &set)
	if strings.Join(set.PrivateWords, ",") != "acme,Beta" || set.HasToken {
		t.Fatalf("sharing = %+v", set)
	}
	h.do("feedback_send", m{"id": f.ID, "preview": true}, &pre)
	if strings.Join(pre.PrivateHits, ",") != "acme" {
		t.Errorf("hits = %v", pre.PrivateHits)
	}
	h.fails("feedback_send", m{"id": f.ID}, "words you keep private (acme)")
	if len(*opened) != 0 || api.posts != 0 {
		t.Fatal("a blocked note is not sent")
	}

	// The private words stay out of diagnostics.
	var d DiagnosticsOut
	h.do("diagnostics_get", nil, &d)
	if _, ok := d.Settings[SettingPrivateWords]; ok {
		t.Error("diagnostics carry the private words")
	}

	// Edited text without the word goes out through the browser, with the marker, and is not
	// marked as sent until the shared list sees it.
	var sent FeedbackSendOut
	h.do("feedback_send", m{"id": f.ID, "title": "[bug] The board flickers", "body": "The board flickers.\n\n---\nKind: bug\nHadron version: test"}, &sent)
	if sent.Via != "browser" || len(*opened) != 1 || !strings.Contains((*opened)[0], "labels=feedback%2Cbug") || !strings.Contains((*opened)[0], "hadron-feedback%3A"+f.ID) || sent.Note.Shared {
		t.Fatalf("browser send = %+v opened %v", sent, *opened)
	}
	api.add("[bug] The board flickers", "The board flickers.\n\n---\nKind: bug\nHadron version: test\n<!-- hadron-feedback:"+f.ID+" -->")
	var list FeedbackListSharedOut
	h.do("feedback_list_shared", m{"refresh": true}, &list)
	if len(list.Issues) != 1 || list.Issues[0].LocalID != f.ID {
		t.Fatalf("shared = %+v", list)
	}
	var got []FeedbackView
	h.do("feedback_list", nil, &got)
	if !got[0].Shared || got[0].GithubURL != "https://github.com/GabiHert/hadron/issues/1" {
		t.Errorf("the browser-sent note gets its link: %+v", got[0])
	}
	h.fails("feedback_send", m{"id": f.ID}, "already on GitHub")
}

func TestFeedbackSendWithTokenRecordsTheIssue(t *testing.T) {
	h, api, tok, opened := shareHarness(t)
	var s FeedbackSharingOut
	h.do("set_feedback_token", m{"token": "github_pat_abcdefghijklmnopqrstuvwxyz"}, &s)
	if !s.HasToken || tok.tok == "" {
		t.Fatalf("sharing = %+v", s)
	}
	var f FeedbackView
	h.do("feedback_create", m{"kind": "idea", "text": "Let me pin a card"}, &f)
	var sent FeedbackSendOut
	h.do("feedback_send", m{"id": f.ID}, &sent)
	if sent.Via != "token" || sent.URL != "https://github.com/GabiHert/hadron/issues/1" || !sent.Note.Shared || len(*opened) != 0 || api.posts != 1 {
		t.Fatalf("sent = %+v", sent)
	}
	h.fails("feedback_send", m{"id": f.ID}, "already on GitHub")
	if api.posts != 1 {
		t.Error("sent twice")
	}
	h.do("set_feedback_token", m{"token": ""}, &s)
	if s.HasToken {
		t.Error("an empty token removes it")
	}
}

func TestFeedbackListAndImportShared(t *testing.T) {
	h, api, _, _ := shareHarness(t)
	api.add("[annoyance] Too many toasts", "Too many toasts\n\n---\nKind: annoyance\nArea: board\nHadron version: v0.2.30\n", "feedback", "annoyance")
	api.add("Not feedback", "a plain issue", "bug")
	var list FeedbackListSharedOut
	h.do("feedback_list_shared", nil, &list)
	if len(list.Issues) != 1 || list.Issues[0].Kind != "annoyance" || list.Issues[0].Version != "v0.2.30" || list.Issues[0].LocalID != "" || list.FetchedAt == "" {
		t.Fatalf("shared = %+v", list)
	}
	var f FeedbackView
	h.do("feedback_import_shared", m{"number": 1}, &f)
	if f.Kind != "annoyance" || f.Text != "Too many toasts" || f.Area != "board" || f.Version != "v0.2.30" || f.GithubURL != "https://github.com/GabiHert/hadron/issues/1" {
		t.Fatalf("imported = %+v", f)
	}
	h.fails("feedback_import_shared", m{"number": 1}, "already in your inbox")
	h.fails("feedback_import_shared", m{"number": 2}, "no shared feedback #2")
	h.do("feedback_list_shared", nil, &list)
	if list.Issues[0].LocalID != f.ID {
		t.Errorf("the imported issue shows its note: %+v", list.Issues[0])
	}
}

func TestFeedbackSharingRefusesCardSessionsAndNeedsTheService(t *testing.T) {
	h, _, _, _ := shareHarness(t)
	var f FeedbackView
	h.do("feedback_create", m{"kind": "bug", "text": "x"}, &f)
	err := h.as(Actor{Kind: KindSession, Name: "alpha"}, "feedback_send", m{"id": f.ID}, nil)
	if err == nil || !strings.Contains(err.Error(), "card session cannot") {
		t.Errorf("card session send: %v", err)
	}
	plain := newHarness(t)
	plain.fails("feedback_list_shared", nil, "not available")
}
