package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"path/filepath"
)

const (
	apiBase  = "https://api.github.com"
	sumsName = "SHA256SUMS"
)

// Release is the newest published release.
type Release struct {
	Tag   string
	Name  string
	Notes string
	Date  time.Time
	URL   string // the release page
	Asset Asset  // the zip for this Mac
	Sums  Asset  // SHA256SUMS
}

// Asset is one file attached to a release.
type Asset struct {
	ID   int64
	Name string
	Size int64
}

// GitHub reads one repo's releases over the REST API. It uses Go's HTTP client, which on macOS
// checks certificates against the system keychain, so a CA installed there is trusted. A private
// repo is read with a token; tokens live only for the request and are never logged.
type GitHub struct {
	Repo, BaseURL string
	Client        *http.Client
	// Tokens returns the tokens to try, in order, once a request without one is refused.
	Tokens func(ctx context.Context, owner string) []string
	// AssetSuffixes pick the release zip, best first, such as "-darwin-arm64.zip".
	AssetSuffixes []string
}

func newGitHub(repo string, tokens func(ctx context.Context, owner string) []string) *GitHub {
	g := &GitHub{Repo: repo, BaseURL: apiBase, Client: &http.Client{}, Tokens: tokens, AssetSuffixes: assetSuffixes()}
	if v := os.Getenv("UPDATER_REPO"); v != "" {
		g.Repo = v
	}
	return g
}

func (g *GitHub) owner() string { o, _, _ := strings.Cut(g.Repo, "/"); return o }

// Latest returns the newest published release that has a zip for this Mac and a SHA256SUMS.
func (g *GitHub) Latest(ctx context.Context) (Release, error) {
	resp, err := g.get(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", g.BaseURL, g.Repo), "application/vnd.github+json")
	if err != nil {
		// GitHub answers 404 both for a repo it hides and for one with no release yet.
		if repo, rerr := g.get(ctx, fmt.Sprintf("%s/repos/%s", g.BaseURL, g.Repo), "application/vnd.github+json"); rerr == nil {
			drain(repo)
			return Release{}, fmt.Errorf("%s has no published release yet.", g.Repo)
		}
		return Release{}, err
	}
	defer resp.Body.Close()
	var body struct {
		Tag         string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil || body.Tag == "" {
		return Release{}, fmt.Errorf("GitHub sent an answer the updater could not read for %s.", g.Repo)
	}
	r := Release{Tag: body.Tag, Name: strings.TrimSpace(body.Name), Notes: strings.TrimSpace(body.Body),
		Date: body.PublishedAt, URL: body.HTMLURL}
	for _, suffix := range g.AssetSuffixes {
		for _, a := range body.Assets {
			if r.Asset.ID == 0 && strings.HasSuffix(a.Name, suffix) {
				r.Asset = Asset{ID: a.ID, Name: a.Name, Size: a.Size}
			}
		}
	}
	for _, a := range body.Assets {
		if a.Name == sumsName {
			r.Sums = Asset{ID: a.ID, Name: a.Name, Size: a.Size}
		}
	}
	if r.Asset.ID == 0 {
		return r, fmt.Errorf("Release %s has no file for this Mac (one ending in %s).", r.Tag, strings.Join(g.AssetSuffixes, " or "))
	}
	if r.Sums.ID == 0 {
		return r, fmt.Errorf("Release %s has no %s, so the download cannot be checked.", r.Tag, sumsName)
	}
	return r, nil
}

// Download passes the asset's bytes to read. The API redirects to GitHub's file host; Go's client
// follows it and drops the Authorization header because the host changes.
func (g *GitHub) Download(ctx context.Context, a Asset, read func(io.Reader) error) error {
	resp, err := g.get(ctx, fmt.Sprintf("%s/repos/%s/releases/assets/%d", g.BaseURL, g.Repo, a.ID), "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return read(resp.Body)
}

// get tries without a token first, then each token; it returns only a 200 response.
func (g *GitHub) get(ctx context.Context, url, accept string) (*http.Response, error) {
	resp, err := g.request(ctx, url, accept, "")
	if err != nil || !denied(resp.StatusCode) {
		return ok(resp, err)
	}
	drain(resp)
	if g.Tokens != nil {
		for _, tok := range g.Tokens(ctx, g.owner()) {
			resp, err := g.request(ctx, url, accept, tok)
			if err != nil || !denied(resp.StatusCode) {
				return ok(resp, err)
			}
			drain(resp)
		}
	}
	return nil, fmt.Errorf("Couldn't read the releases of %s on GitHub. Add a GitHub token below, or make the repo public.", g.Repo)
}

func (g *GitHub) request(ctx context.Context, url, accept, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "release-updater")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Could not reach GitHub: %v", err)
	}
	return resp, nil
}

func denied(code int) bool {
	return code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusNotFound
}

func ok(resp *http.Response, err error) (*http.Response, error) {
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		drain(resp)
		return nil, fmt.Errorf("GitHub answered %s.", resp.Status)
	}
	return resp, nil
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// ghTokens asks gh for the owner's token, then for the active account's; -u picks an account
// without switching. It is the fallback on a machine without a saved token. gh's output is never
// logged.
func ghTokens(ctx context.Context, env []string, owner string) []string {
	gh, err := lookPath(env, "gh")
	if err != nil {
		return nil
	}
	var out []string
	for _, args := range [][]string{{"auth", "token", "-u", owner}, {"auth", "token"}} {
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		cmd := exec.CommandContext(c, gh, args...)
		cmd.Env = env
		b, err := cmd.Output()
		cancel()
		if t := strings.TrimSpace(string(b)); err == nil && t != "" && (len(out) == 0 || out[0] != t) {
			out = append(out, t)
		}
	}
	return out
}

// lookPath finds name on the PATH in env (the login shell's environment), falling back to this
// process's PATH. GUI apps start with a minimal PATH, so env usually comes from the login shell.
func lookPath(env []string, name string) (string, error) {
	for _, kv := range env {
		if p, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, dir := range filepath.SplitList(p) {
				f := filepath.Join(dir, name)
				if fi, err := os.Stat(f); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
					return f, nil
				}
			}
		}
	}
	return exec.LookPath(name)
}
