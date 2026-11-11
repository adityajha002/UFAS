package main

// The admin UI is built into webui/dist and embedded in the binary. It is
// served from the admin listener WITHOUT the bearer token (a browser cannot
// send a header when it loads a page); only /admin/api/ requires the token.

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:webui/dist
var uiFS embed.FS

const uiCSP = "default-src 'self'; img-src 'self' data:; connect-src 'self' https: http:; frame-ancestors 'none'; base-uri 'none'"

func uiRoot() fs.FS {
	sub, err := fs.Sub(uiFS, "webui/dist")
	if err != nil {
		panic(err)
	}
	return sub
}

// uiHandler serves files from fsys; an unknown GET path falls back to
// index.html (single-page app routing), except under /assets/ where a missing
// file must stay a 404.
func uiHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", uiCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		f, err := fsys.Open(name)
		if err == nil {
			if st, serr := f.Stat(); serr != nil || st.IsDir() {
				f.Close()
				err = fs.ErrNotExist
			}
		}
		if err != nil {
			if strings.HasPrefix(name, "assets/") {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			if f, err = fsys.Open(name); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		defer f.Close()
		if strings.HasPrefix(name, "assets/") { // content-hashed by the build
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			b, rerr := io.ReadAll(f)
			if rerr != nil {
				http.Error(w, "internal error", 500)
				return
			}
			rs = bytes.NewReader(b)
		}
		http.ServeContent(w, r, name, time.Time{}, rs)
	})
}
