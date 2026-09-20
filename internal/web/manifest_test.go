package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/web/branding"
)

func TestManifestDefaultsToBrandName(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

	result := response.Result()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if got := result.Header.Get("Content-Type"); got != "application/manifest+json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := result.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}

	var manifest webManifest
	if err := json.NewDecoder(result.Body).Decode(&manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	brand := branding.Default()
	if manifest.Name != brand.Name || manifest.ShortName != brand.Name {
		t.Errorf("Name/ShortName = %q/%q, want %q", manifest.Name, manifest.ShortName, brand.Name)
	}
	if manifest.ID != "/" || manifest.StartURL != "/" || manifest.Scope != "/" {
		t.Errorf("ID/StartURL/Scope = %q/%q/%q, want \"/\" for each", manifest.ID, manifest.StartURL, manifest.Scope)
	}
	if manifest.Display != "standalone" {
		t.Errorf("Display = %q, want %q", manifest.Display, "standalone")
	}
	if manifest.ThemeColor != brand.Colors.DeepOcean || manifest.BackgroundColor != brand.Colors.DeepOcean {
		t.Errorf("ThemeColor/BackgroundColor = %q/%q, want %q", manifest.ThemeColor, manifest.BackgroundColor, brand.Colors.DeepOcean)
	}

	wantIcons := map[string]string{brand.Assets.Icons.Any192: "any", brand.Assets.Icons.Any512: "any", brand.Assets.Icons.Maskable512: "maskable"}
	if len(manifest.Icons) != len(wantIcons) {
		t.Fatalf("Icons = %#v, want %d entries", manifest.Icons, len(wantIcons))
	}
	for _, icon := range manifest.Icons {
		if wantPurpose, ok := wantIcons[icon.Src]; !ok || icon.Purpose != wantPurpose || icon.Type != "image/png" {
			t.Errorf("unexpected icon entry %#v", icon)
		}
	}
}

func TestManifestUsesResolvedInstanceName(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }, InstanceName: "Camp Nova"}).ServeHTTP(response, request)

	var manifest webManifest
	if err := json.NewDecoder(response.Result().Body).Decode(&manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Name != "Camp Nova" || manifest.ShortName != "Camp Nova" {
		t.Errorf("Name/ShortName = %q/%q, want %q", manifest.Name, manifest.ShortName, "Camp Nova")
	}
}

func TestManifestAllowsHead(t *testing.T) {
	request := httptest.NewRequest(http.MethodHead, "/manifest.webmanifest", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

	result := response.Result()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if got := result.Header.Get("Content-Type"); got != "application/manifest+json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if response.Body.Len() != 0 {
		t.Errorf("HEAD response body length = %d, want 0", response.Body.Len())
	}
}

func TestManifestRejectsOtherMethods(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/manifest.webmanifest", nil)
	response := httptest.NewRecorder()

	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }}).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
