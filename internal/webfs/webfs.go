// Package webfs serves the frontend embedded by release builds. Build with the
// embedweb tag after copying web/dist to internal/webfs/dist. Development builds
// omit the tag and continue to serve disk assets or Vite.
package webfs

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves this binary's frontend directly, without extracting files or
// consulting the working directory. Development builds return nil.
func Handler() http.Handler {
	if !hasEmbedded {
		return nil
	}
	tree, err := fs.Sub(embeddedFS, "dist")
	if err != nil {
		// The embedweb build contains this fixed directory.
		panic(err)
	}
	return spaHandler(tree)
}

func spaHandler(tree fs.FS) http.Handler {
	files := http.FileServerFS(tree)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		info, err := fs.Stat(tree, name)
		if err != nil || info.IsDir() || name == "index.html" {
			// Client-side routes share the SPA shell; directories are never listed.
			// Revalidate the shell so a browser cannot retain the previous build.
			w.Header().Set("Cache-Control", "no-cache")
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
