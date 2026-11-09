package script

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// KeychainService is the login keychain service that holds integration secrets.
const KeychainService = "Hadron secrets"

// Secrets stores the secret values integrations use. Keys are "<integration>/<name>", so one
// integration cannot read another's.
type Secrets interface {
	Get(key string) (string, bool, error)
	Set(key, value string) error
	Delete(key string) error
}

// SecretKey is the storage key of an integration's secret.
func SecretKey(integration, name string) string { return integration + "/" + name }

var secretKeyRE = regexp.MustCompile(`^[a-z][a-z0-9_]*/[a-zA-Z][a-zA-Z0-9_]*$`)

func checkKey(key string) error {
	if !secretKeyRE.MatchString(key) {
		return fmt.Errorf("secret key %q must look like <integration>/<name>", key)
	}
	return nil
}

// NewSecrets returns the file store when HADRON_SECRETS=file, else the macOS login keychain.
func NewSecrets(home string) Secrets {
	if os.Getenv("HADRON_SECRETS") == "file" {
		return &FileSecrets{Path: filepath.Join(home, "secrets.json")}
	}
	return &KeychainSecrets{}
}

// FileSecrets keeps secrets in a 0600 JSON file. Tests use it; so can a user without a keychain.
type FileSecrets struct {
	Path string
	mu   sync.Mutex
}

func (f *FileSecrets) load() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", f.Path, err)
	}
	return m, nil
}

func (f *FileSecrets) save(m map[string]string) error {
	b, _ := json.Marshal(m)
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

// Get implements Secrets.
func (f *FileSecrets) Get(key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	v, ok := m[key]
	return v, ok, err
}

// Set implements Secrets.
func (f *FileSecrets) Set(key, value string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[key] = value
	return f.save(m)
}

// Delete implements Secrets.
func (f *FileSecrets) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	delete(m, key)
	return f.save(m)
}

// KeychainSecrets keeps secrets in the macOS login keychain under the service "Hadron secrets".
// The value is stored base64-encoded so any text survives security(1)'s quoting, and it reaches
// security on stdin, never on a command line.
type KeychainSecrets struct {
	Bin string // /usr/bin/security; tests replace it
}

func (k KeychainSecrets) bin() string {
	if k.Bin != "" {
		return k.Bin
	}
	return "/usr/bin/security"
}

// Get implements Secrets.
func (k KeychainSecrets) Get(key string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, k.bin(), "find-generic-password", "-s", KeychainService, "-a", key, "-w").Output()
	if err != nil {
		return "", false, nil // not found
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return "", false, fmt.Errorf("the keychain item for %s is not one Hadron wrote", key)
	}
	return string(raw), true, nil
}

// Set implements Secrets.
func (k KeychainSecrets) Set(key, value string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, k.bin(), "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %q -a %q -l %q -w %s\n",
		KeychainService, key, KeychainService+" ("+key+")", base64.StdEncoding.EncodeToString([]byte(value))))
	if out, err := cmd.CombinedOutput(); err != nil || strings.Contains(string(out), "error") {
		return errors.New("macOS did not save the secret in your keychain")
	}
	return nil
}

// Delete implements Secrets.
func (k KeychainSecrets) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, k.bin(), "delete-generic-password", "-s", KeychainService, "-a", key).Run()
	return nil
}
