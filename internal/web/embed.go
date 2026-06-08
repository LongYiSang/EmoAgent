package web

import (
	"embed"
	"io/fs"
	"net/http"
)

const MissingWebBuildMessage = "web frontend assets are not embedded; run `npm --prefix web run build` before go build"

//go:embed static
var StaticFS embed.FS

func NewStaticHandler(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, "static/dist/index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, MissingWebBuildMessage, http.StatusServiceUnavailable)
		})
	}
	staticSub, err := fs.Sub(fsys, "static/dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "load embedded web assets: "+err.Error(), http.StatusInternalServerError)
		})
	}
	return http.FileServer(http.FS(staticSub))
}
