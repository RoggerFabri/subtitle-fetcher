package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web
var webFS embed.FS

func serveUI(mux *http.ServeMux) {
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))
}
