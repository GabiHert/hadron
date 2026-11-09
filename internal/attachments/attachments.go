// Package attachments keeps the images and PDFs the user attaches to a terminal or a chat message.
// Each file is copied under <home>/attachments/<YYYY-MM-DD>/<uuid>.<ext>, so a Claude Code session
// can read it by its absolute path, and the window can show a thumbnail of it through Handler,
// which serves that folder and nothing else.
package attachments

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxBytes is the largest file Hadron keeps: 20 MB.
const MaxBytes = 20 << 20

// URLPrefix is where the window loads an attachment from: URLPrefix + "<date>/<file>".
const URLPrefix = "/attachments/"

// types maps the content types Hadron accepts, as http.DetectContentType names them, to the
// extension a saved file gets.
var types = map[string]string{
	"image/png":       "png",
	"image/jpeg":      "jpg",
	"image/gif":       "gif",
	"image/webp":      "webp",
	"application/pdf": "pdf",
}

// images are the extensions Handler serves; a PDF is passed to sessions as a path only.
var images = map[string]string{"png": "image/png", "jpg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}

// Saved describes a stored attachment.
type Saved struct {
	Path string `json:"path"`
	URL  string `json:"url"`
	Mime string `json:"mime"`
	Size int    `json:"size"`
	Name string `json:"name"`
}

// Dir is the attachments folder of a Hadron home.
func Dir(home string) string { return filepath.Join(home, "attachments") }

// ErrTooLarge is returned for a file over MaxBytes.
var ErrTooLarge = fmt.Errorf("the file is larger than %d MB, the most Hadron attaches", MaxBytes>>20)

// Save stores data, whose type is read from its bytes (the name is kept only for the result), and
// returns where it went.
func Save(home, name string, data []byte, now time.Time) (Saved, error) {
	if len(data) == 0 {
		return Saved{}, errors.New("the file is empty")
	}
	if len(data) > MaxBytes {
		return Saved{}, ErrTooLarge
	}
	mime := http.DetectContentType(data)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	ext, ok := types[mime]
	if !ok {
		return Saved{}, fmt.Errorf("%s is a %s file: Hadron attaches PNG, JPEG, GIF and WebP images, and PDFs", displayName(name), mime)
	}
	day := now.Local().Format("2006-01-02")
	dir := filepath.Join(Dir(home), day)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Saved{}, err
	}
	file := uuid.NewString() + "." + ext
	p := filepath.Join(dir, file)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return Saved{}, err
	}
	out := Saved{Path: p, Mime: mime, Size: len(data), Name: name}
	if _, img := images[ext]; img {
		out.URL = URLPrefix + day + "/" + file
	}
	return out, nil
}

func displayName(name string) string {
	if name == "" {
		return "This"
	}
	return filepath.Base(name)
}

// relRe is the only shape of path Handler serves: a date folder and a uuid file with an image
// extension. Anything else, including "..", is a 404 before the file system is touched.
var relRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.(png|jpg|gif|webp)$`)

// Handler serves GET URLPrefix<date>/<uuid>.<ext> from home's attachments folder.
func Handler(home string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		m := relRe.FindStringSubmatch(strings.TrimPrefix(r.URL.Path, URLPrefix))
		if m == nil || !strings.HasPrefix(r.URL.Path, URLPrefix) {
			http.NotFound(w, r)
			return
		}
		p := filepath.Join(Dir(home), m[1], m[2]+"."+m[3])
		info, err := os.Lstat(p)
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", images[m[3]])
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		_, _ = w.Write(data)
	})
}

// Middleware routes URLPrefix to Handler and everything else to next, for Wails' asset server.
func Middleware(home string) func(next http.Handler) http.Handler {
	h := Handler(home)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, URLPrefix) {
				h.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
