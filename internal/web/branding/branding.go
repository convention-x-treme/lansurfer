// Package branding contains LANsurfer's fixed brand definition.
package branding

//go:generate go run ./cmd/brandcss -output ../../../web/src/brand.generated.css

// Brand is the fixed product identity consumed by the web layer and asset
// generator. It is deliberately application data rather than configuration.
type Brand struct {
	Name    string
	Tagline string
	Colors  Colors
	Assets  Assets
}

// Colors contains the semantic palette documented in docs/BRANDING.md.
type Colors struct {
	DeepOcean  string
	OceanDepth string
	SurfBlue   string
	AquaWave   string
	SeaFoam    string
	WhiteSand  string
	GoldenHour string
	Sunset     string
	Coral      string
}

// Assets contains the public paths used by templates and generated styles.
type Assets struct {
	Stylesheet  string
	Script      string
	Mascot      string
	PrimaryFont string
	DataFont    string
}

// Default returns the fixed LANsurfer brand.
func Default() Brand {
	return Brand{
		Name:    "LANsurfer",
		Tagline: "Find your crew. Catch the next game.",
		Colors: Colors{
			DeepOcean:  "#0D1B2A",
			OceanDepth: "#1B263B",
			SurfBlue:   "#00B4D8",
			AquaWave:   "#48CAE4",
			SeaFoam:    "#90E0EF",
			WhiteSand:  "#F8F9FA",
			GoldenHour: "#F4D35E",
			Sunset:     "#FF6B35",
			Coral:      "#FF3F73",
		},
		Assets: Assets{
			Stylesheet:  "/assets/app.css",
			Script:      "/assets/app.js",
			Mascot:      "/assets/images/otter.png",
			PrimaryFont: "/assets/fonts/montserrat-latin-wght-normal.woff2",
			DataFont:    "/assets/fonts/jetbrains-mono-latin-wght-normal.woff2",
		},
	}
}
