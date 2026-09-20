package web

import (
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/web/assets"
)

// serviceWorkerHandler serves the compiled service-worker bundle at root
// scope. It must not be served under /assets/*: a worker's maximum allowed
// scope is the path it was served from, and registration uses scope "/".
func serviceWorkerHandler() (http.HandlerFunc, error) {
	body, err := assets.ServiceWorkerScript()
	if err != nil {
		return nil, err
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	}, nil
}
