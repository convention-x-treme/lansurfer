package web

import (
	"encoding/json"
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/web/branding"
)

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

// webManifest is the web app manifest body. Field set is fixed by the
// approved Track D contract: no orientation, categories, shortcuts, or
// screenshots. See docs/superpowers/pwa/2026-09-20-track-d-instance-pwa-design.md.
type webManifest struct {
	Name            string         `json:"name"`
	ShortName       string         `json:"short_name"`
	ID              string         `json:"id"`
	StartURL        string         `json:"start_url"`
	Scope           string         `json:"scope"`
	Display         string         `json:"display"`
	ThemeColor      string         `json:"theme_color"`
	BackgroundColor string         `json:"background_color"`
	Icons           []manifestIcon `json:"icons"`
}

// manifestHandler serves the resolved web app manifest. The body is fixed
// for the process lifetime (instance name and bundled brand/icons do not
// change at runtime), so it is marshaled once at router construction.
func manifestHandler(brand branding.Brand, instanceName string) http.HandlerFunc {
	manifest := webManifest{
		Name: instanceName, ShortName: instanceName,
		ID: "/", StartURL: "/", Scope: "/", Display: "standalone",
		ThemeColor: brand.Colors.DeepOcean, BackgroundColor: brand.Colors.DeepOcean,
		Icons: []manifestIcon{
			{Src: brand.Assets.Icons.Any192, Sizes: "192x192", Type: "image/png", Purpose: "any"},
			{Src: brand.Assets.Icons.Any512, Sizes: "512x512", Type: "image/png", Purpose: "any"},
			{Src: brand.Assets.Icons.Maskable512, Sizes: "512x512", Type: "image/png", Purpose: "maskable"},
		},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		panic("marshal web app manifest: " + err.Error())
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}
}
