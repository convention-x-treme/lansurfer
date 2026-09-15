package assets

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// assetRequest builds a request the way the router delegates it to this
// handler: the "/assets/" prefix is stripped, so the path has no leading slash.
func assetRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, "/assets/"+path, nil)
	request.URL.Path = path
	return request
}

// Browsers must be able to validate and reuse embedded assets. Without cache
// validators they refetch the stylesheet, script, and webfonts on every
// navigation, which renders text in a fallback font until the webfont arrives.
func TestHandlerServesCacheValidatedAssets(t *testing.T) {
	handler := Handler()

	for _, path := range []string{"app.css", "app.js", "fonts/schibsted-grotesk-latin-wght-normal.woff2", "fonts/schibsted-grotesk-latin-wght-italic.woff2", "fonts/jetbrains-mono-latin-wght-normal.woff2"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, assetRequest(http.MethodGet, path))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			etag := response.Header().Get("ETag")
			if etag == "" {
				t.Fatal("ETag header is missing")
			}
			if cacheControl := response.Header().Get("Cache-Control"); cacheControl == "" {
				t.Fatal("Cache-Control header is missing")
			}

			revalidation := httptest.NewRecorder()
			request := assetRequest(http.MethodGet, path)
			request.Header.Set("If-None-Match", etag)
			handler.ServeHTTP(revalidation, request)
			if revalidation.Code != http.StatusNotModified {
				t.Fatalf("revalidation status = %d, want %d", revalidation.Code, http.StatusNotModified)
			}
		})
	}
}

func TestHandlerLeavesUnknownPathsUncached(t *testing.T) {
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, assetRequest(http.MethodGet, "does-not-exist.css"))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "" {
		t.Fatalf("Cache-Control = %q, want none", cacheControl)
	}
}
