package auth

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeProfileKeepsOnlyUsableAvatars(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef"
	tests := []struct {
		kind, ref, want string
	}{
		{"discord", hash, hash},
		{"discord", "a_" + hash, "a_" + hash},
		{"discord", "https://cdn.discordapp.com/x.png", ""},
		{"discord", strings.ToUpper(hash), ""},
		{"twitch", "https://static-cdn.jtvnw.net/u/p.png", "https://static-cdn.jtvnw.net/u/p.png"},
		{"twitch", "https://evil.example/p.png", ""},
		{"twitch", "http://static-cdn.jtvnw.net/u/p.png", ""},
		{"oidc", "https://id.example/p.png", "https://id.example/p.png"},
		{"oidc", "https://id.example:8443/p.png", ""},
		{"oidc", "https://user@id.example/p.png", ""},
		{"oidc", "https://id.example/" + strings.Repeat("a", maxAvatarRefLength), ""},
		{"oidc", "  ", ""},
	}
	for _, test := range tests {
		profile := normalizeProfile(Profile{ProviderKind: test.kind, ProviderKey: test.kind, Subject: "s", AvatarRef: test.ref})
		if profile.AvatarRef != test.want {
			t.Errorf("%s %q: AvatarRef = %q, want %q", test.kind, test.ref, profile.AvatarRef, test.want)
		}
		if err := validateProfile(profile); err != nil {
			t.Errorf("%s %q: avatar made profile invalid: %v", test.kind, test.ref, err)
		}
	}
}

func TestAvatarKeyChangesWithPicture(t *testing.T) {
	first := AvatarKey("discord", "1", "0123456789abcdef0123456789abcdef")
	second := AvatarKey("discord", "1", "fedcba9876543210fedcba9876543210")
	if len(first) != 32 || first == second || AvatarKey("discord", "1", "") != "" {
		t.Fatalf("keys = %q %q", first, second)
	}
}

type fakeAvatarCatalog struct{ avatar StoredAvatar }

func (c fakeAvatarCatalog) SelectedAvatarKnown(context.Context, string) (bool, error) {
	return true, nil
}
func (c fakeAvatarCatalog) SelectedAvatarByKey(context.Context, string) (StoredAvatar, error) {
	return c.avatar, nil
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestAvatarSourceFetchesDiscordURLFromStoredHashOnly(t *testing.T) {
	image := pngBytes(t)
	var path string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.RequestURI()
		_, _ = w.Write(image)
	}))
	defer server.Close()
	source := newAvatarSource(fakeAvatarCatalog{StoredAvatar{"discord", "42", "0123456789abcdef0123456789abcdef"}}, server.URL, &net.Dialer{})
	source.client.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig
	got, err := source.Fetch(t.Context(), "avatar", "key")
	if err != nil || !bytes.Equal(got, image) {
		t.Fatalf("Fetch = %d bytes, %v", len(got), err)
	}
	if path != "/avatars/42/0123456789abcdef0123456789abcdef.png?size=128" {
		t.Fatalf("upstream path = %q", path)
	}
}

func TestAvatarSourceRejectsBadUpstreamResponses(t *testing.T) {
	image := pngBytes(t)
	tests := map[string]http.HandlerFunc{
		"redirect":  func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) },
		"not found": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"html":      func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>hi</html>")) },
		"oversize": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(append(image, make([]byte, maxAvatarBytes)...))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewTLSServer(handler)
			defer server.Close()
			source := newAvatarSource(fakeAvatarCatalog{StoredAvatar{"discord", "42", "0123456789abcdef0123456789abcdef"}}, server.URL, &net.Dialer{})
			source.client.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig
			if _, err := source.Fetch(t.Context(), "avatar", "key"); err == nil {
				t.Fatal("Fetch error = nil")
			}
		})
	}
}

func TestAvatarSourceRefusesPrivateAddresses(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("guarded dialer reached a loopback server")
	}))
	defer server.Close()
	source := newAvatarSource(fakeAvatarCatalog{StoredAvatar{"discord", "42", "0123456789abcdef0123456789abcdef"}}, server.URL, guardedDialer())
	_, err := source.Fetch(t.Context(), "avatar", "key")
	if err == nil || !strings.Contains(err.Error(), "not public") {
		t.Fatalf("Fetch error = %v, want non-public refusal", err)
	}
}

func TestAvatarSourceRevalidatesStoredReference(t *testing.T) {
	source := newAvatarSource(fakeAvatarCatalog{StoredAvatar{"twitch", "1", "https://evil.example/p.png"}}, "https://unused", guardedDialer())
	if _, err := source.Fetch(t.Context(), "avatar", "key"); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch error = %v, want stored-reference rejection", err)
	}
}
