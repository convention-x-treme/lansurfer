#!/bin/sh
set -eu

# Probe from the same Compose network as LANsurfer, without published host ports.
attempt=0
while [ "$attempt" -lt 45 ]; do
  if curl --fail --silent --max-time 3 http://lansurfer:8080/readyz >/dev/null; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 2
done

if [ "$attempt" -ge 45 ]; then
  echo "LANsurfer did not become ready within 90 seconds" >&2
  exit 1
fi
test "$(curl --fail --silent http://lansurfer:8080/healthz)" = ok
test "$(curl --fail --silent http://lansurfer:8080/readyz)" = ready
css_type="$(curl --fail --silent --show-error -o /dev/null -w '%{content_type}' http://lansurfer:8080/assets/app.css)"
case "$css_type" in text/css*) ;; *) echo "Unexpected CSS content type: $css_type" >&2; exit 1 ;; esac
js_type="$(curl --fail --silent --show-error -o /dev/null -w '%{content_type}' http://lansurfer:8080/assets/app.js)"
case "$js_type" in text/javascript*|application/javascript*) ;; *) echo "Unexpected JavaScript content type: $js_type" >&2; exit 1 ;; esac
license="$(curl --fail --silent --show-error http://lansurfer:8080/assets/licenses/htmx-0BSD.txt)"
case "$license" in *"Zero-Clause BSD"*) ;; *) echo "Bundled HTMX license text is missing" >&2; exit 1 ;; esac
echo "LANsurfer Compose smoke checks passed."
