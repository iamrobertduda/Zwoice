package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"strings"
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

// DownloadHandler exposes only the public website and its assets, never the local UI/API.
func DownloadHandler() http.Handler {
	public := map[string]string{"/download.css": "download.css", "/download.js": "download.js", "/icon.svg": "icon.svg", "/archivo-black-latin.woff2": "archivo-black-latin.woff2", "/dm-sans-latin.woff2": "dm-sans-latin.woff2"}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if file, ok := public[r.URL.Path]; ok {
			http.ServeFileFS(w, r, files, "assets/"+file)
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Vary", "Accept-Language")
			http.Redirect(w, r, "/"+preferredLanguage(r.Header.Get("Accept-Language"))+"/", http.StatusFound)
			return
		}
		code, rest, slash := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
		if _, ok := siteStrings[code]; !ok || rest != "" {
			http.NotFound(w, r)
			return
		}
		if !slash {
			http.Redirect(w, r, "/"+code+"/", http.StatusMovedPermanently)
			return
		}
		var page bytes.Buffer
		if err := siteTemplate.Execute(&page, newSitePage(code, siteOrigin(r))); err != nil {
			http.Error(w, "page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Language", code)
		w.Write(page.Bytes())
	})
}
