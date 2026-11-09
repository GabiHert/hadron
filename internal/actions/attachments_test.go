package actions

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/attachments"
)

func TestAttachmentSaveStoresTheUsersImageAndRefusesSessions(t *testing.T) {
	h := newHarness(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 16)...)
	var out attachments.Saved
	h.do("attachment_save", m{"name": "shot.png", "data": base64.StdEncoding.EncodeToString(png)}, &out)
	home := h.reg.Deps().Home
	if !strings.HasPrefix(out.Path, home+"/attachments/") || out.Mime != "image/png" || out.URL == "" {
		t.Fatalf("saved %+v", out)
	}
	if _, err := os.Stat(out.Path); err != nil {
		t.Fatal(err)
	}
	h.fails("attachment_save", m{"data": "not base64!"}, "not base64")
	h.fails("attachment_save", m{"name": "a.txt", "data": base64.StdEncoding.EncodeToString([]byte("hello"))}, "a.txt is a text/plain file")
	err := h.as(Actor{Kind: KindSession, Name: "alpha"}, "attachment_save", m{"data": base64.StdEncoding.EncodeToString(png)}, nil)
	if err == nil || !strings.Contains(err.Error(), "only the user may use attachment_save") {
		t.Fatalf("session: %v", err)
	}
}
