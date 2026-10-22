// Package branding contains LANsurfer's fixed brand definition.
package branding

import (
	"regexp"
	"strings"

	"github.com/convention-x-treme/lansurfer/internal/web/assets"
)

//go:generate go run ./cmd/brandcss -output ../../../web/src/brand.generated.css

// Operator attribution. Every "Powered by …" credit in the interface renders
// from these two values, so deploying changes only this file. This is the
// operating community, not the LANsurfer product identity.
const (
	OperatorName = "Convention-X-Treme"
	OperatorURL  = "https://www.convention-x-treme.de"
)

// RepositoryURL and IssuesURL belong to the software, not the deployment.
const (
	RepositoryURL = "https://github.com/convention-x-treme/lansurfer"
	IssuesURL     = RepositoryURL + "/issues"
)

var releaseVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

// ReleaseURL returns the GitHub release-notes URL for a tagged build version,
// or "" for development and snapshot builds, which have no published release.
func ReleaseURL(version string) string {
	if !releaseVersion.MatchString(version) || strings.Contains(version, "SNAPSHOT") {
		return ""
	}
	return RepositoryURL + "/releases/tag/" + version
}

// Brand is the fixed product identity consumed by the web layer and asset
// generator. It is deliberately application data rather than configuration.
type Brand struct {
	Name    string
	Tagline string
	Colors  Colors
	Assets  Assets
}

// Colors contains the semantic palette documented in DESIGN.md.
type Colors struct {
	DeepOcean  string
	OceanDepth string
	Harbour    string
	DeepBlue   string
	SurfBlue   string
	AquaWave   string
	SeaFoam    string
	SeaMist    string
	TideLine   string
	WhiteSand  string
	GoldenHour string
	Sunset     string
	Coral      string
}

// Assets contains the public paths used by templates and generated styles.
type Assets struct {
	Stylesheet string
	// IconStylesheet is the bundled Phosphor Regular + Fill webfont stylesheet.
	IconStylesheet string
	Script         string
	Mascot         string
	// MascotSad is shown on empty and no-match states (Game Requests, Games, admin catalog).
	MascotSad string
	// PrimaryFontItalic carries the wordmark, which is the only italic in the
	// system, so it is preloaded beside the upright face.
	PrimaryFont       string
	PrimaryFontItalic string
	DataFont          string
	Icons             Icons
	// AdminPWADiagnostics is the progressive-enhancement script for the
	// read-only /admin/pwa page. It is not part of the always-loaded Script.
	AdminPWADiagnostics string
}

// Icons contains the public paths for the bundled application-icon
// renditions the manifest and HTML head reference. These are fixed bundled
// assets, not instance-configurable state: DESIGN.md records how they
// are rendered from docs/logo/icon.png.
type Icons struct {
	Any192        string
	Any512        string
	Maskable512   string
	AppleTouch180 string
	Favicon16     string
	Favicon32     string
	FaviconICO    string
}

// Default returns the fixed LANsurfer brand.
func Default() Brand {
	return Brand{
		Name:    "LANsurfer",
		Tagline: "Find your crew. Catch the next game.",
		Colors: Colors{
			DeepOcean:  "#0D1B2A",
			OceanDepth: "#1B263B",
			Harbour:    "#163F57",
			DeepBlue:   "#0077A8",
			SurfBlue:   "#00B4D8",
			AquaWave:   "#48CAE4",
			SeaFoam:    "#90E0EF",
			SeaMist:    "#E1EDF4",
			TideLine:   "#BED3DF",
			WhiteSand:  "#F8F9FA",
			GoldenHour: "#F4D35E",
			Sunset:     "#FF6B35",
			Coral:      "#FF3F73",
		},
		Assets: Assets{
			Stylesheet:        assets.VersionedURL("app.css"),
			IconStylesheet:    assets.VersionedURL("phosphor.css"),
			Script:            assets.VersionedURL("app.js"),
			Mascot:            "/assets/images/otter.webp",
			MascotSad:         "/assets/images/otter-sad.webp",
			PrimaryFont:       "/assets/fonts/schibsted-grotesk-latin-wght-normal.woff2",
			PrimaryFontItalic: "/assets/fonts/schibsted-grotesk-latin-wght-italic.woff2",
			DataFont:          "/assets/fonts/jetbrains-mono-latin-wght-normal.woff2",
			Icons: Icons{
				Any192:        "/assets/images/icons/icon-192.png",
				Any512:        "/assets/images/icons/icon-512.png",
				Maskable512:   "/assets/images/icons/icon-512-maskable.png",
				AppleTouch180: "/assets/images/icons/apple-touch-icon-180.png",
				Favicon16:     "/assets/images/icons/favicon-16.png",
				Favicon32:     "/assets/images/icons/favicon-32.png",
				FaviconICO:    "/assets/images/icons/favicon.ico",
			},
			AdminPWADiagnostics: assets.VersionedURL("admin-pwa.js"),
		},
	}
}
