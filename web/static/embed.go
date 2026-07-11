// Package static embeds web/static assets into the binary so that the
// production image does not depend on files on disk. Use Assets() to obtain
// an http.FileSystem backed by the embedded tree.
package static

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed css js img
var embedded embed.FS

//go:embed img/favicon.svg
var faviconBytes []byte

// SubFS returns the embedded static assets as an fs.FS rooted at the static dir.
func SubFS() fs.FS {
	sub, err := fs.Sub(embedded, ".")
	if err != nil {
		panic(err)
	}
	return sub
}

// Handler returns an http.Handler serving the embedded assets. It should be
// mounted with http.StripPrefix("/static/", ...).
func Handler() http.Handler {
	return http.FileServer(http.FS(SubFS()))
}

// Favicon returns the embedded favicon.svg bytes for direct serving at /favicon.ico.
func Favicon() []byte { return faviconBytes }
