package handler

import "testing"

func TestDeviceLabelFromUserAgent(t *testing.T) {
	tests := map[string]string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36":                             "Chrome on Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36 Edg/120.0":                   "Edge on Windows",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0":                                                        "Firefox on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15":                   "Safari on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1": "Safari on iPhone",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36":                       "Chrome on Android",
		"Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0":                                                                  "Firefox on Linux",
		"":       "This device",
		"curl/8": "This device",
	}
	for ua, want := range tests {
		if got := deviceLabelFromUserAgent(ua); got != want {
			t.Errorf("%q = %q, want %q", ua, got, want)
		}
	}
}
