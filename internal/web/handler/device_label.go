package handler

import "strings"

// deviceLabelFromUserAgent names a push device like "Chrome on Windows" so
// several registered browsers are distinguishable. Substring matching only;
// order matters because most browsers embed other browsers' tokens.
// ponytail: naive UA sniffing, unknown agents fall back to "This device".
func deviceLabelFromUserAgent(ua string) string {
	browser := firstMatch(ua, [][2]string{
		{"Edg/", "Edge"}, {"EdgA/", "Edge"}, {"EdgiOS/", "Edge"}, {"OPR/", "Opera"},
		{"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"},
		{"SamsungBrowser/", "Samsung Internet"},
		{"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	})
	// iPhone/iPad UAs contain "like Mac OS X", so check them before macOS.
	system := firstMatch(ua, [][2]string{
		{"Windows", "Windows"}, {"Android", "Android"}, {"iPhone", "iPhone"},
		{"iPad", "iPad"}, {"Mac OS X", "macOS"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"},
	})
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	case system != "":
		return system
	}
	return "This device"
}

func firstMatch(s string, pairs [][2]string) string {
	for _, p := range pairs {
		if strings.Contains(s, p[0]) {
			return p[1]
		}
	}
	return ""
}
