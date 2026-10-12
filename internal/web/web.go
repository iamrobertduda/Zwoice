package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets/*
var files embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}

// DownloadHandler exposes only public download assets, never the local UI/API.
func DownloadHandler() http.Handler {
	public := map[string]string{"/": "download.html", "/download.css": "download.css", "/download.js": "download.js", "/icon.svg": "icon.svg", "/archivo-black-latin.woff2": "archivo-black-latin.woff2", "/dm-sans-latin.woff2": "dm-sans-latin.woff2"}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, ok := public[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.ServeFileFS(w, r, files, "assets/"+file)
	})
}
