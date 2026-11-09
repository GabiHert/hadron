package attachments

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

func TestSaveKeepsAnImageUnderTheDateFolder(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.Local)
	s, err := Save(home, "shot.png", pngBytes, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Path, filepath.Join(home, "attachments", "2026-11-09")+string(filepath.Separator)) || !strings.HasSuffix(s.Path, ".png") {
		t.Fatalf("path %q", s.Path)
	}
	if s.Mime != "image/png" || s.Size != len(pngBytes) || !strings.HasPrefix(s.URL, "/attachments/2026-11-09/") {
		t.Fatalf("saved %+v", s)
	}
	got, _ := os.ReadFile(s.Path)
	if !bytes.Equal(got, pngBytes) {
		t.Fatal("content differs")
	}
	if info, _ := os.Stat(s.Path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestSaveAcceptsAPDFWithoutAURL(t *testing.T) {
	s, err := Save(t.TempDir(), "spec.pdf", []byte("%PDF-1.7\n..."), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s.Mime != "application/pdf" || s.URL != "" || !strings.HasSuffix(s.Path, ".pdf") {
		t.Fatalf("saved %+v", s)
	}
}

func TestSaveRefusesOtherTypesEmptyAndOversized(t *testing.T) {
	home := t.TempDir()
	if _, err := Save(home, "run.sh", []byte("#!/bin/sh\necho hi\n"), time.Now()); err == nil || !strings.Contains(err.Error(), "run.sh is a text/plain file") {
		t.Fatalf("text: %v", err)
	}
	if _, err := Save(home, "x.png", nil, time.Now()); err == nil {
		t.Fatal("empty accepted")
	}
	big := append(append([]byte{}, pngBytes...), make([]byte, MaxBytes)...)
	if _, err := Save(home, "big.png", big, time.Now()); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big: %v", err)
	}
}

func TestHandlerServesOnlyAttachmentImages(t *testing.T) {
	home := t.TempDir()
	s, err := Save(home, "a.png", pngBytes, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A file next to the attachments folder must never be reachable.
	_ = os.WriteFile(filepath.Join(home, "hadron.db"), []byte("secret"), 0o600)
	pdf, _ := Save(home, "b.pdf", []byte("%PDF-1.7\n"), time.Now())
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("next")) })
	h := Middleware(home)(next)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get(s.URL); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" || !bytes.Equal(rec.Body.Bytes(), pngBytes) {
		t.Fatalf("image: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, p := range []string{
		"/attachments/../hadron.db",
		"/attachments/%2e%2e/hadron.db",
		"/attachments/" + filepath.Base(filepath.Dir(pdf.Path)) + "/" + filepath.Base(pdf.Path),
		"/attachments/2026-11-09/not-a-uuid.png",
		"/attachments/",
	} {
		if rec := get(p); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := get("/index.html"); rec.Body.String() != "next" {
		t.Fatal("other paths must go to the asset server")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, s.URL, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post: %d", rec.Code)
	}
}
