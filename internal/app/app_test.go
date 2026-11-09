package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/store"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	loginEnv = os.Environ // never read the developer's login shell in a test
	home := t.TempDir()
	t.Setenv("HADRON_HOME", home)
	st, err := store.Open(filepath.Join(home, "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	a := New(st)
	t.Cleanup(a.shutdown)
	return a
}

// ptyBridge lists the methods the terminal bridge may bind (spec section 3).
var ptyBridge = map[string]bool{"PtyAttach": true, "PtyWrite": true, "PtyResize": true}

func TestOnlyInvokeAndThePTYBridgeAreBound(t *testing.T) {
	var bound []string
	typ := reflect.TypeOf(&App{})
	for i := 0; i < typ.NumMethod(); i++ {
		bound = append(bound, typ.Method(i).Name)
	}
	sort.Strings(bound)
	foundInvoke := false
	for _, name := range bound {
		if name == "Invoke" {
			foundInvoke = true
			continue
		}
		if !ptyBridge[name] {
			t.Errorf("App.%s is exported, so Wails would bind it. Make it an action, or unexport it", name)
		}
	}
	if !foundInvoke {
		t.Error("App.Invoke is missing")
	}
}

func TestInvokeRunsActionsAsYou(t *testing.T) {
	a := newTestApp(t)
	out, err := a.Invoke("app_status", "")
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Actions       int `json:"actions"`
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil || st.Actions == 0 || st.SchemaVersion < 3 {
		t.Fatalf("app_status = %s, %v", out, err)
	}
	if _, err := a.Invoke("no_such_action", "{}"); err == nil {
		t.Fatal("an unknown action should fail")
	}
	_ = context.Background()
}

func TestPtyBridgeAttachWriteResize(t *testing.T) {
	a := newTestApp(t)
	mustInvoke := func(name, args string) string {
		t.Helper()
		out, err := a.Invoke(name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	mustInvoke("settings_update", `{"values":{"session.command":"/bin/cat"}}`)
	var ws struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(mustInvoke("workspace_create", `{"name":"W","root":"`+t.TempDir()+`"}`)), &ws)
	mustInvoke("card_create", `{"workspace_id":"`+ws.ID+`","nick":"cat-card","launch_profile":"plain"}`)
	var st struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	_ = json.Unmarshal([]byte(mustInvoke("session_status", `{"card":"cat-card"}`)), &st)
	id := st.Session.ID
	if id == "" {
		t.Fatal("no session id")
	}
	deadline := time.Now().Add(5 * time.Second)
	for a.PtyWrite(id, base64.StdEncoding.EncodeToString([]byte("hello bridge\r"))) != nil {
		if time.Now().After(deadline) {
			t.Fatal("the session never accepted input")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var replay string
	for time.Now().Before(deadline) {
		end, b64, _ := strings.Cut(a.PtyAttach(id), ":")
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatal(err)
		}
		if n, _ := strconv.Atoi(end); n != len(raw) {
			t.Fatalf("PtyAttach end offset %s, replay is %d bytes", end, len(raw))
		}
		replay = string(raw)
		if strings.Count(replay, "hello bridge") >= 2 { // the tty echo, then cat's output
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if strings.Count(replay, "hello bridge") < 2 {
		t.Fatalf("replay = %q", replay)
	}
	if err := a.PtyResize(id, 90, 30); err != nil {
		t.Fatal(err)
	}
	if err := a.PtyResize(id, 0, 30); err == nil {
		t.Fatal("a zero width should be refused")
	}
	if err := a.PtyWrite(id, "%%%not base64"); err == nil {
		t.Fatal("PtyWrite should refuse data that is not base64")
	}
	if err := a.PtyWrite("no-such-session", ""); err == nil {
		t.Fatal("writing to an unknown session should fail")
	}
	if got := a.PtyAttach("no-such-session"); got != "0:" {
		t.Fatalf("attaching to an unknown session = %q, want an empty replay", got)
	}
}

func TestPtyFeedBatchesOutputWithOffsets(t *testing.T) {
	var mu sync.Mutex
	var got []PtyData
	f := &ptyFeeds{byID: map[string]*ptyFeed{}, emit: func(event string, data any) {
		mu.Lock()
		defer mu.Unlock()
		if event != "pty:data:s1" {
			t.Errorf("event = %s", event)
		}
		got = append(got, data.(PtyData))
	}}
	f.add("s1", []byte("abc"), 3)
	f.add("s1", []byte("de"), 5)
	time.Sleep(60 * time.Millisecond)
	f.add("s1", []byte("f"), 6)
	f.flush("s1") // an attach flushes first, so the replay and the events line up
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("events = %+v, want two batches", got)
	}
	d0, _ := base64.StdEncoding.DecodeString(got[0].Data)
	if got[0].Start != 0 || got[0].End != 5 || string(d0) != "abcde" || got[1].Start != 5 || got[1].End != 6 {
		t.Fatalf("events = %+v", got)
	}
}

// The WI-09 check, minus the window: a plain launch profile on /bin/zsh, "echo hi" typed through the
// bridge the terminal uses, and the output read back from the screen model.
func TestPlainProfileOnZshEchoesHi(t *testing.T) {
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("no /bin/zsh")
	}
	a := newTestApp(t)
	inv := func(name, args string) string {
		t.Helper()
		out, err := a.Invoke(name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return out
	}
	inv("settings_update", `{"values":{"session.command":"/bin/zsh"}}`)
	var ws struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(inv("workspace_create", `{"name":"W","root":"`+t.TempDir()+`"}`)), &ws)
	var profs []struct{ ID, Name string }
	_ = json.Unmarshal([]byte(inv("launch_profile_list", `{"workspace_id":"`+ws.ID+`"}`)), &profs)
	for _, p := range profs {
		if p.Name == "plain" {
			inv("launch_profile_update", `{"profile_id":"`+p.ID+`","env":{"ZDOTDIR":"`+t.TempDir()+`","PS1":"$ "}}`)
		}
	}
	inv("card_create", `{"workspace_id":"`+ws.ID+`","nick":"zsh-card","launch_profile":"plain"}`)
	var st struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	_ = json.Unmarshal([]byte(inv("session_status", `{"card":"zsh-card"}`)), &st)
	deadline := time.Now().Add(8 * time.Second)
	time.Sleep(500 * time.Millisecond)
	_ = a.PtyWrite(st.Session.ID, base64.StdEncoding.EncodeToString([]byte("echo hi\r")))
	for time.Now().Before(deadline) {
		var out struct {
			Screen string `json:"screen"`
		}
		_ = json.Unmarshal([]byte(inv("session_read_screen", `{"card":"zsh-card"}`)), &out)
		for _, l := range strings.Split(out.Screen, "\n") {
			if strings.TrimSpace(l) == "hi" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("echo hi never showed as a line of its own on the screen")
}

// TestSessionMCPConfigNamesThePortTheServerReallyGot occupies the preferred port, so the server
// falls back to another, and checks the per-session config points at that one.
func TestSessionMCPConfigNamesThePortTheServerReallyGot(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port

	a := newTestApp(t)
	t.Setenv("HADRON_MCP_PORT", strconv.Itoa(busyPort))
	if err := a.mcp.Start(); err != nil { // what startup does first
		t.Fatal(err)
	}
	port := a.mcp.Port()
	if port == 0 || port == busyPort {
		t.Fatalf("server port = %d, busy port = %d", port, busyPort)
	}
	if err := a.st.Settings.Set("session.command", json.RawMessage(`"/bin/cat"`)); err != nil {
		t.Fatal(err)
	}
	var ws struct{ ID string }
	out, err := a.Invoke("workspace_create", `{"name":"Acme","root":"`+t.TempDir()+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(out), &ws)
	if _, err := a.Invoke("card_create", `{"nick":"x","workspace_id":"`+ws.ID+`"}`); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(run.Home(), "run", "*.mcp.json"))
	if len(files) == 0 {
		t.Fatal("no session MCP config was written")
	}
	b, _ := os.ReadFile(files[0])
	if want := "http://127.0.0.1:" + strconv.Itoa(port) + "/mcp"; !strings.Contains(string(b), want) {
		t.Errorf("config = %s, want url %s", b, want)
	}
}
