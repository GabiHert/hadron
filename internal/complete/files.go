package complete

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// File is one file or folder the composer offers after "@".
type File struct {
	// Path is relative to the session's folder, with forward slashes.
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

// Limits of a file listing.
const (
	// MaxEntries is the most files and folders one listing holds.
	MaxEntries = 20000
	// DefaultLimit and MaxLimit bound how many results a search returns.
	DefaultLimit = 50
	MaxLimit     = 200
	// cacheFor is how long a listing is reused before the folder is read again.
	cacheFor = 10 * time.Second
)

// skipDirs are folders a walk of a folder that is not a git repository never enters.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".hadron-worktrees": true, "dist": true, "build": true, "out": true,
	"target": true, ".next": true, ".nuxt": true, ".svelte-kit": true, ".turbo": true, ".cache": true,
	"coverage": true, "vendor": true, ".venv": true, "venv": true, "__pycache__": true, ".gradle": true,
	"DerivedData": true, ".idea": true, ".pytest_cache": true, ".mypy_cache": true, ".terraform": true,
}

// ErrOutside is returned for a query that names a path outside the folder.
var ErrOutside = errors.New("outside the folder")

type listing struct {
	at      time.Time
	entries []File
	git     bool
}

// Files lists and searches folders, keeping each listing for a few seconds.
type Files struct {
	mu    sync.Mutex
	cache map[string]listing
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// NewFiles returns an empty file searcher.
func NewFiles() *Files { return &Files{cache: map[string]listing{}, Now: time.Now} }

// Search returns up to limit files and folders of root that match q, best first. An empty q
// lists the shallowest entries. q may not be absolute or climb out with "..".
func (f *Files) Search(root, q string, limit int) ([]File, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	q = strings.TrimSpace(strings.Trim(strings.TrimSpace(q), `"`))
	if filepath.IsAbs(q) || strings.HasPrefix(q, "~") || hasDotDot(q) {
		return nil, fmt.Errorf("%w: search for a path inside %s, without a leading / or ~ and without ..", ErrOutside, root)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("the folder %s cannot be read: %w", root, err)
	}
	if st, err := os.Stat(real); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", root)
	}
	l, err := f.list(real)
	if err != nil {
		return nil, err
	}
	q = strings.TrimPrefix(filepath.ToSlash(q), "./")
	type hit struct {
		f     File
		score int
	}
	var hits []hit
	if q == "" {
		for _, e := range l.entries {
			hits = append(hits, hit{f: e, score: -strings.Count(e.Path, "/")*100 - len(e.Path)})
		}
	} else {
		for _, e := range l.entries {
			if s, ok := Score(q, e.Path); ok {
				hits = append(hits, hit{f: e, score: s})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].f.Path < hits[j].f.Path
	})
	out := make([]File, 0, limit)
	for _, h := range hits {
		if len(out) == limit {
			break
		}
		if !staysInside(real, h.f.Path) {
			continue
		}
		out = append(out, h.f)
	}
	return out, nil
}

// Forget drops the cached listing of a folder.
func (f *Files) Forget(root string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if real, err := filepath.EvalSymlinks(root); err == nil {
		delete(f.cache, real)
	}
	delete(f.cache, root)
}

func (f *Files) list(real string) (listing, error) {
	f.mu.Lock()
	if l, ok := f.cache[real]; ok && f.Now().Sub(l.at) < cacheFor {
		f.mu.Unlock()
		return l, nil
	}
	f.mu.Unlock()
	entries, isGit := gitList(real)
	if !isGit {
		entries = walkList(real)
	}
	l := listing{at: f.Now(), entries: entries, git: isGit}
	f.mu.Lock()
	if len(f.cache) > 64 {
		f.cache = map[string]listing{}
	}
	f.cache[real] = l
	f.mu.Unlock()
	return l, nil
}

// gitList lists the tracked files and the untracked ones git does not ignore, relative to root,
// and the folders that hold them. ok is false when root is not in a git work tree.
func gitList(root string) ([]File, bool) {
	inside, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(inside)) != "true" {
		return nil, false
	}
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, false
	}
	dirs := map[string]bool{}
	seen := map[string]bool{}
	var files []File
	for _, p := range strings.Split(out.String(), "\x00") {
		if p == "" || seen[p] || hasDotDot(p) {
			continue
		}
		seen[p] = true
		for d := filepath.ToSlash(filepath.Dir(p)); d != "." && d != "/" && !dirs[d]; d = filepath.ToSlash(filepath.Dir(d)) {
			dirs[d] = true
		}
		files = append(files, File{Path: p})
		if len(files) >= MaxEntries {
			break
		}
	}
	entries := make([]File, 0, len(dirs)+len(files))
	for d := range dirs {
		entries = append(entries, File{Path: d, IsDir: true})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	entries = append(entries, files...)
	if len(entries) > MaxEntries {
		entries = entries[:MaxEntries]
	}
	return entries, true
}

// walkList walks root without following symlinks, skipping dependency, build and hidden VCS
// folders, up to MaxEntries entries.
func walkList(root string) []File {
	var out []File
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if len(out) >= MaxEntries {
			return filepath.SkipAll
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDirs[d.Name()] || d.Name() == ".DS_Store" {
				return filepath.SkipDir
			}
			out = append(out, File{Path: rel, IsDir: true})
			return nil
		}
		if d.Name() == ".DS_Store" {
			return nil
		}
		out = append(out, File{Path: rel})
		return nil
	})
	return out
}

// staysInside reports whether rel, under root, is not a symlink that leads out of root.
func staysInside(root, rel string) bool {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if !within(root, p) {
		return false
	}
	st, err := os.Lstat(p)
	if err != nil {
		// A tracked file deleted from the work tree is still a name git knows; offer it.
		return !errors.Is(err, fs.ErrPermission)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return st.Mode()&fs.ModeSymlink == 0
	}
	return within(root, real)
}

func hasDotDot(p string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(p), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

// Score rates how well q matches s, fuzzily and ignoring case: every character of q must appear in
// s in order. Matches in the last path segment, at the start of a word and in a row score higher,
// and a shorter s wins a tie. ok is false when q does not match.
func Score(q, s string) (int, bool) {
	ql, sl := []rune(strings.ToLower(q)), []rune(strings.ToLower(s))
	if len(ql) == 0 {
		return 0, true
	}
	base := strings.LastIndexByte(s, '/') + 1
	baseRunes := len([]rune(s[:base]))
	// A plain substring is the best kind of match.
	if i := strings.Index(string(sl), string(ql)); i >= 0 {
		ri := len([]rune(string(sl)[:i]))
		score := 1000 - len(sl)
		if ri >= baseRunes {
			score += 400
			if ri == baseRunes {
				score += 300
			}
		} else if ri == 0 || isSep(sl[ri-1]) {
			score += 150
		}
		return score, true
	}
	score, qi, prev := 0, 0, -2
	for si := 0; si < len(sl) && qi < len(ql); si++ {
		if sl[si] != ql[qi] {
			continue
		}
		pts := 1
		if si == prev+1 {
			pts += 6
		}
		if si == 0 || isSep(sl[si-1]) {
			pts += 8
		}
		if si >= baseRunes {
			pts += 3
		}
		score += pts
		prev = si
		qi++
	}
	if qi < len(ql) {
		return 0, false
	}
	return score*10 - len(sl), true
}

func isSep(r rune) bool { return r == '/' || r == '-' || r == '_' || r == '.' || r == ' ' || r == ':' }
