package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

//go:embed site/download.html site/i18n/*.json
var siteFiles embed.FS

// siteLanguages is the switcher order; the first entry is the fallback for
// browsers that ask for none of them.
var siteLanguages = []string{"en", "de", "fr", "nl", "it", "es", "da", "no", "sv", "pl", "pt"}

var siteStrings = loadSiteStrings()

// Translations are trusted, embedded files. Only strings passed through raw may
// carry markup (<em>, <br />, <strong>); everything else is escaped.
var siteTemplate = template.Must(template.New("download.html").Option("missingkey=error").Funcs(template.FuncMap{
	"raw":  func(s string) template.HTML { return template.HTML(s) },
	"fill": func(s, os string) string { return strings.ReplaceAll(s, "{os}", os) },
}).ParseFS(siteFiles, "site/download.html"))

type siteLink struct {
	Code, Name string
	Current    bool
}

type sitePage struct {
	Lang, Origin string
	T            map[string]string
	Languages    []siteLink
}

func loadSiteStrings() map[string]map[string]string {
	all := make(map[string]map[string]string, len(siteLanguages))
	for _, code := range siteLanguages {
		data, err := siteFiles.ReadFile("site/i18n/" + code + ".json")
		if err != nil {
			panic(err)
		}
		var t map[string]string
		if err := json.Unmarshal(data, &t); err != nil {
			panic(fmt.Errorf("site/i18n/%s.json: %w", code, err))
		}
		all[code] = t
	}
	return all
}

func newSitePage(code, origin string) sitePage {
	links := make([]siteLink, len(siteLanguages))
	for i, c := range siteLanguages {
		links[i] = siteLink{Code: c, Name: siteStrings[c]["languageName"], Current: c == code}
	}
	return sitePage{Lang: code, Origin: origin, T: siteStrings[code], Languages: links}
}

// preferredLanguage picks the supported language with the highest q-value;
// regional variants (de-AT, pt-BR) and Norwegian nb/nn map to their base page.
func preferredLanguage(header string) string {
	best, bestQ := siteLanguages[0], 0.0
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				continue
			}
			q = f
		}
		base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if base == "nb" || base == "nn" {
			base = "no"
		}
		if _, ok := siteStrings[base]; ok && q > bestQ {
			best, bestQ = base, q
		}
	}
	return best
}

// siteOrigin builds absolute URLs for canonical and hreflang links. Unexpected
// Host values fall back to root-relative links instead of being echoed back.
func siteOrigin(r *http.Request) string {
	host := strings.ToLower(r.Host)
	if host == "" || strings.ContainsFunc(host, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == ':' || c == '[' || c == ']')
	}) {
		return ""
	}
	name := host
	if i := strings.LastIndex(host, ":"); i > strings.LastIndex(host, "]") {
		name = host[:i]
	}
	if name == "localhost" || name == "127.0.0.1" || name == "[::1]" {
		return "http://" + host
	}
	return "https://" + host
}
