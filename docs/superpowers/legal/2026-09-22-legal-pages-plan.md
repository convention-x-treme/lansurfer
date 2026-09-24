# Public Terms, Privacy, and Contact/Reporting Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve public `/terms` and `/privacy` pages filled from env-configured operator legal identity, and link them plus a contact/reporting path from a footer in every page shell.

**Architecture:** `internal/platform/config` loads and validates four `LANSURFER_LEGAL_*`/`LANSURFER_IMPRINT_URL` values. A `middleware.Legal` carries them on the request context (mirroring `middleware.Instance`), so the shared `components.Footer` and the new `pages.Terms`/`pages.Privacy` read them without new handler parameters. A small `handler.LegalHandler` renders the pages inside the normal `Layout`.

**Tech Stack:** Go, chi, templ v0.3.1020, Tailwind 4 + DaisyUI 5.

**Spec:** [`2026-09-22-legal-pages-design.md`](2026-09-22-legal-pages-design.md)

## Global Constraints

- Env names exactly: `LANSURFER_LEGAL_ENTITY_NAME`, `LANSURFER_LEGAL_ADDRESS`, `LANSURFER_LEGAL_CONTACT_EMAIL`, `LANSURFER_IMPRINT_URL`.
- Entity name ≤ 200 runes, address ≤ 300 runes, both single line without control characters; email is a bare address (`net/mail`); imprint is an absolute `http`/`https` URL.
- Discord login configured + empty entity name or contact email = startup error.
- Unset values render as `not configured`; imprint link is omitted when unset.
- Routes `GET /terms`, `GET /privacy`; public, no session required. Contact section anchor `id="contact"`; footer link `/terms#contact`.
- Footer is not `position: fixed`/`sticky`; shells use a `min-h-dvh` flex column with `main` as `flex-1`.
- English text only. No new dependencies. Generated `*_templ.go` files are regenerated, never hand-edited.
- Issues URL constant: `https://github.com/convention-x-treme/lansurfer/issues`.
- `branding.OperatorName`/`OperatorURL` ("Powered by …") stay unchanged.

## Review Focus

1. Existing developer `.env` with Discord credentials but no legal values stops booting — expected; `.env.example` and README must say so (Task 5), and the error must name both variables (Task 1 test).
2. Contact email written as `Name <mail@x>` or with spaces — must be rejected, not rendered into a `mailto:` (Task 1 test).
3. Imprint URL like `javascript:alert(1)` or `<https://…>` placeholder — must be rejected at startup (Task 1 test).
4. Signed-in player opening `/privacy` — must get the app shell with sidebar, not the anonymous header (Task 4 test).
5. Login screen on a short viewport — footer must not push the login panel off-screen or overlap it; the `.auth-stage` min-height change must keep the stage filling the viewport above the footer (Task 3 manual check).

---

### Task 1: Legal configuration

**Files:**
- Modify: `internal/platform/config/config.go`
- Test: `internal/platform/config/config_test.go`

**Interfaces:**
- Produces: `config.Legal{EntityName, Address, ContactEmail, ImprintURL string}` and field `config.Config.Legal Legal`.

- [ ] **Step 1: Write failing tests**

Add the four variables to `clearOptionalEnvironment`'s list:

```go
		"LANSURFER_LEGAL_ENTITY_NAME",
		"LANSURFER_LEGAL_ADDRESS",
		"LANSURFER_LEGAL_CONTACT_EMAIL",
		"LANSURFER_IMPRINT_URL",
```

In `TestLoadReadsConfiguredValues` (it configures Discord, which now requires legal values), add before `Load()`:

```go
	t.Setenv("LANSURFER_LEGAL_ENTITY_NAME", " Computerfreunde Karlsdorf-Neuthard e.V. ")
	t.Setenv("LANSURFER_LEGAL_CONTACT_EMAIL", " datenschutz@convention-x-treme.de ")
```

Append new tests:

```go
func TestLoadReadsLegalIdentity(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_LEGAL_ENTITY_NAME", " Computerfreunde Karlsdorf-Neuthard e.V. ")
	t.Setenv("LANSURFER_LEGAL_ADDRESS", " Example Street 1, 76689 Karlsdorf-Neuthard, Germany ")
	t.Setenv("LANSURFER_LEGAL_CONTACT_EMAIL", " datenschutz@convention-x-treme.de ")
	t.Setenv("LANSURFER_IMPRINT_URL", " https://www.convention-x-treme.de/impressum ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := Legal{
		EntityName:   "Computerfreunde Karlsdorf-Neuthard e.V.",
		Address:      "Example Street 1, 76689 Karlsdorf-Neuthard, Germany",
		ContactEmail: "datenschutz@convention-x-treme.de",
		ImprintURL:   "https://www.convention-x-treme.de/impressum",
	}
	if cfg.Legal != want {
		t.Errorf("Legal = %#v, want %#v", cfg.Legal, want)
	}
}

func TestLoadLeavesLegalIdentityUnsetByDefault(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Legal != (Legal{}) {
		t.Errorf("Legal = %#v, want zero value", cfg.Legal)
	}
}

func TestLoadRejectsInvalidLegalIdentity(t *testing.T) {
	tests := map[string]struct{ name, value string }{
		"entity with line break":  {"LANSURFER_LEGAL_ENTITY_NAME", "Club\ne.V."},
		"entity too long":         {"LANSURFER_LEGAL_ENTITY_NAME", strings.Repeat("a", 201)},
		"address with tab":        {"LANSURFER_LEGAL_ADDRESS", "Street 1,\tCity"},
		"address too long":        {"LANSURFER_LEGAL_ADDRESS", strings.Repeat("a", 301)},
		"email with display name": {"LANSURFER_LEGAL_CONTACT_EMAIL", "Privacy <datenschutz@convention-x-treme.de>"},
		"email without domain":    {"LANSURFER_LEGAL_CONTACT_EMAIL", "datenschutz"},
		"imprint javascript":      {"LANSURFER_IMPRINT_URL", "javascript:alert(1)"},
		"imprint placeholder":     {"LANSURFER_IMPRINT_URL", "<https://www.convention-x-treme.de/impressum>"},
		"imprint relative":        {"LANSURFER_IMPRINT_URL", "/impressum"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			clearOptionalEnvironment(t)
			t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
			t.Setenv(test.name, test.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.name) {
				t.Fatalf("Load() error = %v, want error naming %s", err, test.name)
			}
		})
	}
}

// Discord's Developer Terms require a privacy policy that names a responsible
// party with a contact, so enabling Discord login without one is refused.
func TestLoadRequiresLegalIdentityWithDiscord(t *testing.T) {
	for name, legal := range map[string][2]string{
		"both missing":  {"", ""},
		"email missing": {"Computerfreunde Karlsdorf-Neuthard e.V.", ""},
		"name missing":  {"", "datenschutz@convention-x-treme.de"},
	} {
		t.Run(name, func(t *testing.T) {
			clearOptionalEnvironment(t)
			t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
			t.Setenv("LANSURFER_DISCORD_CLIENT_ID", "client")
			t.Setenv("LANSURFER_DISCORD_CLIENT_SECRET", "secret")
			t.Setenv("LANSURFER_LEGAL_ENTITY_NAME", legal[0])
			t.Setenv("LANSURFER_LEGAL_CONTACT_EMAIL", legal[1])

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "LANSURFER_LEGAL_ENTITY_NAME") ||
				!strings.Contains(err.Error(), "LANSURFER_LEGAL_CONTACT_EMAIL") {
				t.Fatalf("Load() error = %v, want error naming both legal variables", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/platform/config/`
Expected: FAIL — `undefined: Legal`.

- [ ] **Step 3: Implement**

In `config.go` add `"net/mail"` to imports and the constants:

```go
	maxLegalEntityNameLength = 200
	maxLegalAddressLength    = 300
```

Add the type after `Twitch`:

```go
// Legal is the operator's legal identity shown on the Terms and Privacy
// pages. Each deployment is its own data controller, so this is operator
// configuration; it is deliberately not a branding surface.
type Legal struct {
	EntityName   string
	Address      string
	ContactEmail string
	ImprintURL   string
}
```

Add to `Config` after `InstanceName`:

```go
	// Legal is the trimmed, validated operator legal identity. Empty fields
	// mean unset; the pages render them as "not configured".
	Legal Legal
```

Replace `loadInstanceName` with a shared single-line check and add `loadLegal`:

```go
func loadInstanceName() (string, error) {
	name := strings.TrimSpace(os.Getenv("LANSURFER_INSTANCE_NAME"))
	return name, checkSingleLine("LANSURFER_INSTANCE_NAME", name, maxInstanceNameLength)
}

// checkSingleLine rejects control characters, Unicode line/paragraph
// separators, and values longer than max runes. Empty values pass.
func checkSingleLine(name, value string, max int) error {
	for _, r := range value {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return fmt.Errorf("%s must not contain control characters or line breaks", name)
		}
	}
	if length := utf8.RuneCountInString(value); length > max {
		return fmt.Errorf("%s must be %d Unicode characters or fewer, got %d", name, max, length)
	}
	return nil
}

func loadLegal() (Legal, error) {
	legal := Legal{
		EntityName:   strings.TrimSpace(os.Getenv("LANSURFER_LEGAL_ENTITY_NAME")),
		Address:      strings.TrimSpace(os.Getenv("LANSURFER_LEGAL_ADDRESS")),
		ContactEmail: strings.TrimSpace(os.Getenv("LANSURFER_LEGAL_CONTACT_EMAIL")),
		ImprintURL:   strings.TrimSpace(os.Getenv("LANSURFER_IMPRINT_URL")),
	}
	if err := checkSingleLine("LANSURFER_LEGAL_ENTITY_NAME", legal.EntityName, maxLegalEntityNameLength); err != nil {
		return Legal{}, err
	}
	if err := checkSingleLine("LANSURFER_LEGAL_ADDRESS", legal.Address, maxLegalAddressLength); err != nil {
		return Legal{}, err
	}
	if legal.ContactEmail != "" {
		address, err := mail.ParseAddress(legal.ContactEmail)
		if err != nil || address.Name != "" || address.Address != legal.ContactEmail {
			return Legal{}, errors.New("LANSURFER_LEGAL_CONTACT_EMAIL must be a bare email address")
		}
	}
	if legal.ImprintURL != "" {
		parsed, err := url.Parse(legal.ImprintURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return Legal{}, errors.New("LANSURFER_IMPRINT_URL must be an absolute HTTP(S) URL")
		}
	}
	return legal, nil
}
```

Before verifying: the old `loadInstanceName` loop compares against two literal characters that render as spaces; open the file and confirm they are U+2028/U+2029 (e.g. `sed -n '/func loadInstanceName/,/^}/p' config.go | od -c | head`). If they are something else, use those exact characters in `checkSingleLine` instead.

In `Load`, after `loadInstanceName`:

```go
	legal, err := loadLegal()
	if err != nil {
		return Config{}, err
	}
	if discord != nil && (legal.EntityName == "" || legal.ContactEmail == "") {
		return Config{}, errors.New("LANSURFER_LEGAL_ENTITY_NAME and LANSURFER_LEGAL_CONTACT_EMAIL are required when Discord login is configured")
	}
```

and add `Legal: legal,` to the returned `Config`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/platform/config/`
Expected: PASS (existing instance-name tests included).

- [ ] **Step 5: Commit**

```bash
git add internal/platform/config/
git commit -m "feat(config): load operator legal identity from environment"
```

---

### Task 2: Legal identity middleware

**Files:**
- Create: `internal/web/middleware/legal.go`
- Test: `internal/web/middleware/legal_test.go`

**Interfaces:**
- Produces: `middleware.LegalIdentity{EntityName, Address, ContactEmail, ImprintURL string}`, `middleware.Legal(LegalIdentity) func(http.Handler) http.Handler`, `middleware.LegalInfo(context.Context) LegalIdentity`.

- [ ] **Step 1: Write failing test**

```go
package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLegalCarriesIdentity(t *testing.T) {
	want := LegalIdentity{EntityName: "Club e.V.", ContactEmail: "privacy@example.test"}
	var got LegalIdentity
	handler := Legal(want)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = LegalInfo(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got != want {
		t.Errorf("LegalInfo(ctx) = %#v, want %#v", got, want)
	}
}

func TestLegalInfoWithoutMiddlewareIsEmpty(t *testing.T) {
	if got := LegalInfo(context.Background()); got != (LegalIdentity{}) {
		t.Errorf("LegalInfo(ctx) = %#v, want zero value", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/middleware/ -run Legal`
Expected: FAIL — `undefined: LegalIdentity`.

- [ ] **Step 3: Implement**

```go
package middleware

import (
	"context"
	"net/http"
)

type legalIdentityKey struct{}

// LegalIdentity is the operator's configured legal identity: the web layer's
// copy of config.Legal, so templates do not import configuration.
type LegalIdentity struct {
	EntityName   string
	Address      string
	ContactEmail string
	ImprintURL   string
}

// Legal carries the operator's legal identity into every request's context,
// the same way Instance carries the instance name, so the shared footer and
// the Terms/Privacy pages read it without a parameter at every call site.
func Legal(identity LegalIdentity) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), legalIdentityKey{}, identity)))
		})
	}
}

// LegalInfo returns the identity carried by Legal's middleware, or the zero
// value when the middleware was never applied.
func LegalInfo(ctx context.Context) LegalIdentity {
	identity, _ := ctx.Value(legalIdentityKey{}).(LegalIdentity)
	return identity
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/web/middleware/ -run Legal`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/middleware/legal.go internal/web/middleware/legal_test.go
git commit -m "feat(web): carry operator legal identity in request context"
```

---

### Task 3: Footer in every shell and login consent line

**Files:**
- Modify: `internal/web/branding/branding.go` (issues URL constant)
- Modify: `internal/web/components/layout.templ` (`Layout`, `AuthLayout`, `Footer`)
- Modify: `internal/web/pages/authentication_error.templ` (`Login`)
- Modify: `web/src/app.css` (`.auth-stage`)
- Regenerate: `*_templ.go`, `internal/web/assets/dist/*`
- Test: `internal/web/components/layout_test.go`, `internal/web/router_test.go`

**Interfaces:**
- Consumes: `webmiddleware.LegalInfo(ctx)` from Task 2.
- Produces: `branding.IssuesURL`; `components.Footer(brand)` rendering the legal links.

- [ ] **Step 1: Write failing tests**

Append to `layout_test.go`:

```go
// legalContext returns a request context carrying the given legal identity,
// the way NewRouter's Legal middleware does in production.
func legalContext(identity webmiddleware.LegalIdentity) context.Context {
	var carried context.Context
	webmiddleware.Legal(identity)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		carried = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	return carried
}

// Discord requires Terms, Privacy, and a reporting path to be reachable from
// every surface, including the login screen where Discord is offered.
func TestEveryShellLinksLegalPages(t *testing.T) {
	brand := branding.Default()
	shells := map[string]templ.Component{
		"public":        Layout(brand, "Privacy Policy", NavigationState{}),
		"authenticated": Layout(brand, "Game Requests", NavigationState{Viewer: &Viewer{ID: "u1", GameNick: "Ada"}}),
		"auth":          AuthLayout(brand, "Log in"),
	}
	for name, shell := range shells {
		t.Run(name, func(t *testing.T) {
			var rendered bytes.Buffer
			ctx := legalContext(webmiddleware.LegalIdentity{ImprintURL: "https://club.example.test/impressum"})
			if err := shell.Render(ctx, &rendered); err != nil {
				t.Fatalf("render: %v", err)
			}
			body := rendered.String()
			for _, want := range []string{
				`<footer`, `href="/terms"`, `href="/privacy"`, `href="/terms#contact"`,
				`href="https://club.example.test/impressum"`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("shell does not contain %q", want)
				}
			}
		})
	}
}

func TestFooterOmitsImprintWhenUnset(t *testing.T) {
	var rendered bytes.Buffer
	if err := Footer(branding.Default()).Render(legalContext(webmiddleware.LegalIdentity{}), &rendered); err != nil {
		t.Fatalf("render Footer: %v", err)
	}
	if strings.Contains(rendered.String(), "Imprint") {
		t.Error("footer renders an Imprint link without a configured imprint URL")
	}
}
```

Add `"github.com/a-h/templ"` to the test imports.

In `router_test.go` `TestRootRendersLoginLandingPage`, add to the `want` list:

```go
		`href="/terms"`,
		`href="/privacy"`,
		"By logging in you agree to the",
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/components/ ./internal/web/ -run 'Legal|Imprint|Login'`
Expected: FAIL — missing `href="/terms"`.

- [ ] **Step 3: Implement**

`branding.go`, after the operator const block:

```go
// IssuesURL is the software project's public issue tracker, where players
// report LANsurfer bugs. It belongs to the software, not the deployment.
const IssuesURL = "https://github.com/convention-x-treme/lansurfer/issues"
```

`layout.templ` — authenticated shell: replace

```templ
					<div class="min-w-0">
```

with

```templ
					<div class="flex min-h-dvh min-w-0 flex-col">
```

change `<main id="main-content" class="min-w-0">` to `<main id="main-content" class="min-w-0 flex-1">`, and add `@Footer(brand)` directly after that `</main>`.

`AuthLayout` — replace `<main id="main-content">{ children... }</main>` with:

```templ
			<div class="flex min-h-dvh flex-col">
				<main id="main-content" class="flex flex-1 flex-col">{ children... }</main>
				@Footer(brand)
			</div>
```

Replace the `Footer` component:

```templ
// Footer links the Terms, Privacy, and contact/reporting surfaces on every
// shell. It is ordinary flow content, not fixed or sticky: each shell is a
// full-height flex column, so it rests at the viewport bottom on short pages
// without covering content or the live-notice toasts on long ones.
templ Footer(brand branding.Brand) {
	<footer class="border-t border-base-300 bg-base-200">
		<div class="page-shell flex flex-col gap-2 py-4 text-sm text-base-content/70 sm:flex-row sm:items-center sm:justify-between">
			<nav class="flex flex-wrap gap-x-4 gap-y-1" aria-label="Legal">
				<a class="link link-hover" href="/terms">Terms</a>
				<a class="link link-hover" href="/privacy">Privacy</a>
				<a class="link link-hover" href="/terms#contact">Contact &amp; reporting</a>
				if imprint := webmiddleware.LegalInfo(ctx).ImprintURL; imprint != "" {
					<a class="link link-hover" href={ templ.URL(imprint) } rel="noopener noreferrer">Imprint</a>
				}
			</nav>
			@OperatorAttribution("")
		</div>
	</footer>
}
```

`brand` stays a parameter so call sites do not change; it is unused inside `Footer` now. If `go vet` or templ complains about the unused parameter, keep it anyway (templ components allow unused parameters).

`authentication_error.templ` `Login` — inside `if len(providers) > 0 { … }`, after the `for` loop, add:

```templ
					<p class="mt-5 text-xs text-base-content/70">
						By logging in you agree to the <a class="link" href="/terms">Terms of Service</a> and acknowledge the <a class="link" href="/privacy">Privacy Policy</a>.
					</p>
```

`web/src/app.css` `.auth-stage` — replace `min-height: 100dvh;` with `flex: 1;`, so the stage fills the viewport above the footer instead of pushing the footer below the fold.

Regenerate:

Run: `task frontend:build`
Expected: templ reports updated files; CSS bundle rebuilt.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/...`
Expected: PASS.

- [ ] **Step 5: Manual check (Review Focus 5)**

Run `task run` with a local database (or `task dev`) and open `/` at 1280×800 and 375×667. Expected: login stage fills the viewport above the footer; footer visible after scrolling at most one footer height; login panel not overlapped. Open a signed-in screen: footer below content, sidebar unchanged.

- [ ] **Step 6: Commit**

```bash
git add internal/web/branding/branding.go internal/web/components/ internal/web/pages/authentication_error.templ internal/web/pages/authentication_error_templ.go web/src/app.css internal/web/assets/dist internal/web/router_test.go
git commit -m "feat(web): link terms, privacy and contact from every page shell"
```

---

### Task 4: Terms and Privacy pages

**Files:**
- Create: `internal/web/pages/legal.templ` (+ generated `legal_templ.go`)
- Create: `internal/web/handler/legal.go`
- Modify: `internal/web/router.go` (dependency, middleware, routes)
- Modify: `internal/app/app.go` (wiring)
- Test: `internal/web/router_test.go`

**Interfaces:**
- Consumes: `config.Config.Legal` (Task 1), `webmiddleware.Legal`/`LegalInfo`/`LegalIdentity` (Task 2), `branding.IssuesURL` (Task 3), `handler.render`, `handler.Navigation.For`.
- Produces: `pages.Terms(brand, components.NavigationState)`, `pages.Privacy(brand, components.NavigationState)`, `handler.NewLegal(*Navigation, branding.Brand) *LegalHandler`, `RouterDependencies.Legal webmiddleware.LegalIdentity`.

- [ ] **Step 1: Write failing tests**

Append to `router_test.go`:

```go
// The Discord Developer Portal needs stable public Terms and Privacy URLs, so
// both pages must render for an anonymous visitor with the operator's details.
func TestLegalPagesArePublicAndShowOperatorIdentity(t *testing.T) {
	authentication := &routerAuthentication{}
	router := NewRouter(RouterDependencies{
		Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication,
		Legal: webmiddleware.LegalIdentity{
			EntityName:   "Computerfreunde Karlsdorf-Neuthard e.V.",
			Address:      "Example Street 1, 76689 Karlsdorf-Neuthard, Germany",
			ContactEmail: "datenschutz@convention-x-treme.de",
			ImprintURL:   "https://www.convention-x-treme.de/impressum",
		},
	})
	for path, wants := range map[string][]string{
		"/privacy": {"Privacy Policy", "Last updated", "Core-Backbone GmbH", "30 days", "Discord"},
		"/terms":   {"Terms of Service", "Last updated", `id="contact"`, `href="https://github.com/convention-x-treme/lansurfer/issues"`},
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			body := response.Body.String()
			for _, want := range append(wants,
				"Computerfreunde Karlsdorf-Neuthard e.V.",
				"Example Street 1, 76689 Karlsdorf-Neuthard, Germany",
				`href="mailto:datenschutz@convention-x-treme.de"`,
				`href="/auth/login"`,
			) {
				if !strings.Contains(body, want) {
					t.Errorf("body does not contain %q", want)
				}
			}
		})
	}
}

func TestLegalPagesMarkUnconfiguredIdentity(t *testing.T) {
	authentication := &routerAuthentication{}
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication}).
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/privacy", nil))

	body := response.Body.String()
	if !strings.Contains(body, "not configured") {
		t.Error("privacy page does not mark missing operator details as not configured")
	}
	if strings.Contains(body, "mailto:") {
		t.Error("privacy page renders a mailto link without a configured contact email")
	}
}

func TestLegalPagesUseAppShellForSignedInPlayer(t *testing.T) {
	authentication := &routerAuthentication{account: user.User{ID: "u1", GameNick: "Ada Player"}}
	request := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "opaque-session"})
	response := httptest.NewRecorder()
	NewRouter(RouterDependencies{Ready: func(context.Context) error { return nil }, Authentication: authentication, Sessions: authentication}).
		ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, "data-authenticated-shell") || strings.Contains(body, `href="/auth/login"`) {
		t.Error("signed-in player did not get the application shell on /privacy")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run LegalPages`
Expected: FAIL — `unknown field Legal in struct literal`.

- [ ] **Step 3: Implement the pages**

Create `internal/web/pages/legal.templ`:

```templ
package pages

import (
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

// legalLastUpdated is the date shown on both documents. Change it whenever
// either text changes.
const legalLastUpdated = "22 September 2026"

// orNotConfigured renders an unset operator value visibly instead of leaving
// a gap a reader could mistake for complete text.
func orNotConfigured(value string) string {
	if value == "" {
		return "not configured"
	}
	return value
}

templ legalDocument(title string) {
	<div class="page-shell py-10">
		<article class="grid max-w-3xl gap-6 [&_h2]:mt-4 [&_h2]:text-xl [&_h2]:font-bold [&_p]:text-base-content/80 [&_ul]:grid [&_ul]:list-disc [&_ul]:gap-1 [&_ul]:pl-6 [&_ul]:text-base-content/80">
			<header>
				<h1 class="text-3xl font-black tracking-tight">{ title }</h1>
				<p class="mt-2 text-sm">Last updated: { legalLastUpdated }</p>
			</header>
			{ children... }
		</article>
	</div>
}

templ legalContactEmail() {
	if email := webmiddleware.LegalInfo(ctx).ContactEmail; email != "" {
		<a class="link" href={ templ.URL("mailto:" + email) }>{ email }</a>
	} else {
		not configured
	}
}

templ legalOperator() {
	{{ legal := webmiddleware.LegalInfo(ctx) }}
	<p>
		{ orNotConfigured(legal.EntityName) }<br/>
		{ orNotConfigured(legal.Address) }<br/>
		Email: @legalContactEmail()
	</p>
	if legal.ImprintURL != "" {
		<p>Imprint: <a class="link" href={ templ.URL(legal.ImprintURL) } rel="noopener noreferrer">{ legal.ImprintURL }</a></p>
	}
}

templ Privacy(brand branding.Brand, navigation components.NavigationState) {
	@components.Layout(brand, "Privacy Policy", navigation) {
		@legalDocument("Privacy Policy") {
			<p>This policy explains which personal data this LANsurfer instance processes, why, for how long, and how you can exercise your rights under the EU General Data Protection Regulation (GDPR).</p>
			<h2>1. Controller and contact</h2>
			<p>The controller responsible for this LANsurfer instance is:</p>
			@legalOperator()
			<p>For any privacy question or request, including deletion of your data, write to the email address above.</p>
			<h2>2. What LANsurfer stores</h2>
			<ul>
				<li><strong>Account:</strong> an internal user ID, your Game Nick, your role (player or administrator), and when the account was created and last changed.</li>
				<li><strong>Linked login accounts:</strong> for each account you log in with (OpenID Connect, Discord, or Twitch), the provider type, the provider's user ID, and the display name the provider reports. The display name is private account information and may be offered as a Game Nick suggestion; it is never shown to other players. An email address is stored only when a generic OpenID Connect provider supplies one.</li>
				<li><strong>Discord:</strong> when you log in with Discord, LANsurfer requests only the <code>identify</code> scope and stores your Discord user ID and username or global display name. It never requests your Discord email address and does not keep Discord access or refresh tokens after the login completes.</li>
				<li><strong>Sessions:</strong> a hashed session token with its creation, last-use, and expiry times. A session expires after 7 days.</li>
				<li><strong>Login flows:</strong> short-lived login state, including the provider details above, valid for 10 minutes and removed after it expires.</li>
				<li><strong>Your activity:</strong> Game Requests you create, the Game Requests you join or leave, the Games you follow, the Game Requests you recently viewed, and the history of changes to each Game Request.</li>
				<li><strong>Server logs:</strong> for each request, your IP address, the request method and path, and a request ID. Logs are kept for at most 30 days and then deleted.</li>
			</ul>
			<h2>3. Cookies</h2>
			<p>LANsurfer sets only cookies it needs to work: a session cookie, a security (CSRF) cookie, a short-lived cookie that binds a login to your browser, and a cookie that remembers your light or dark theme choice. It uses no analytics, advertising, or tracking cookies.</p>
			<h2>4. Purposes and legal bases</h2>
			<ul>
				<li>Providing your account, login, and the Game Request service: Art. 6(1)(b) GDPR.</li>
				<li>Keeping the service secure and preventing abuse through sessions and server logs: Art. 6(1)(f) GDPR, based on our legitimate interest in a secure service.</li>
			</ul>
			<h2>5. Recipients and third parties</h2>
			<ul>
				<li><strong>Login providers:</strong> when you log in, you are sent to the provider you choose (the configured OpenID Connect provider, Discord, or Twitch). That provider's own privacy policy applies to its processing.</li>
				<li><strong>Hosting:</strong> we process all data ourselves on our own server, housed in a colocation space at the Nuremberg (Germany) data center of Core-Backbone GmbH. Core-Backbone provides only space, power, and network connectivity and has no access to the server, its host system, its virtual machines, or the data. Only we have access.</li>
				<li><strong>Game covers:</strong> the server fetches game cover images from IGDB. Your browser never contacts IGDB, and no data about you is sent to it.</li>
			</ul>
			<p>We do not sell your data or use it for advertising.</p>
			<h2>6. Retention and deletion</h2>
			<p>We keep your account data while your account exists. Unlinking a login account from your account removes that linked account's data. To delete your whole account or your Discord data, write to the contact email above; we will delete it unless the law requires us to keep it.</p>
			<h2>7. Your rights</h2>
			<p>You have the right to access (Art. 15), rectification (Art. 16), erasure (Art. 17), restriction of processing (Art. 18), data portability (Art. 20), and to object (Art. 21). To exercise them, write to the contact email above. You may also lodge a complaint with a data protection supervisory authority, for example the Landesbeauftragter für den Datenschutz und die Informationsfreiheit Baden-Württemberg.</p>
			<h2>8. Security</h2>
			<p>Connections are encrypted with TLS. Session tokens are stored only as hashes, and login provider tokens are not stored.</p>
			<h2>9. Changes</h2>
			<p>We update this policy when LANsurfer's data processing changes. The date at the top shows the latest version.</p>
		}
	}
}

templ Terms(brand branding.Brand, navigation components.NavigationState) {
	@components.Layout(brand, "Terms of Service", navigation) {
		@legalDocument("Terms of Service") {
			<h2>1. The service</h2>
			<p>This LANsurfer instance helps players at our LAN events find each other for games. It is operated by:</p>
			@legalOperator()
			<p>The service is free. We aim to keep it available but do not guarantee availability.</p>
			<h2>2. Accounts</h2>
			<p>You log in with an existing account from a supported provider (OpenID Connect, Discord, or Twitch). That provider's terms continue to apply to your account there. You choose a Game Nick, which is your public name on this instance.</p>
			<h2>3. Acceptable use</h2>
			<p>Do not:</p>
			<ul>
				<li>impersonate another person, including through your Game Nick;</li>
				<li>harass, threaten, or discriminate against other players;</li>
				<li>post hateful, illegal, or sexually explicit content, or spam;</li>
				<li>interfere with the service or try to access data that is not yours.</li>
			</ul>
			<h2>4. Your content</h2>
			<p>Your Game Nick and the Game Requests you create or join are visible to other players of this instance. You are responsible for what you post.</p>
			<h2>5. Moderation</h2>
			<p>We may remove content or suspend accounts that break these terms.</p>
			<h2>6. Liability</h2>
			<p>We are liable without limitation for intent and gross negligence and for injury to life, body, or health. Otherwise our liability is limited as far as German law permits.</p>
			<h2>7. Governing law</h2>
			<p>German law applies.</p>
			<h2>8. Changes</h2>
			<p>We may update these terms. The date at the top shows the latest version.</p>
			<h2 id="contact">9. Contact and reporting</h2>
			<p>For support, to report abuse or a policy violation, for issues with the Discord integration, or for data requests, email @legalContactEmail().</p>
			<p>To report a bug in the LANsurfer software, open an issue on <a class="link" href={ templ.URL(branding.IssuesURL) } rel="noopener noreferrer">GitHub</a>.</p>
		}
	}
}
```

If templ rejects a component call inline inside a text run (`Email: @legalContactEmail()`), wrap the call in its own `<span>`: `<span>Email: </span>@legalContactEmail()`.

- [ ] **Step 4: Implement the handler, routes, and wiring**

Create `internal/web/handler/legal.go`:

```go
package handler

import (
	"net/http"

	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
)

// LegalHandler renders the public Terms of Service and Privacy Policy. Both
// are public so the Discord Developer Portal can link them; a signed-in
// player still gets the application shell.
type LegalHandler struct {
	navigation *Navigation
	brand      branding.Brand
}

func NewLegal(navigation *Navigation, brand branding.Brand) *LegalHandler {
	return &LegalHandler{navigation: navigation, brand: brand}
}

func (h *LegalHandler) Terms(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.Terms(h.brand, h.state(r)))
}

func (h *LegalHandler) Privacy(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, pages.Privacy(h.brand, h.state(r)))
}

func (h *LegalHandler) state(r *http.Request) components.NavigationState {
	state := h.navigation.For(r, "")
	state.LoginAvailable = state.Viewer == nil
	return state
}
```

Add `"github.com/convention-x-treme/lansurfer/internal/web/components"` to its imports.

`router.go`:
- In `RouterDependencies`, after `InstanceName`:

```go
	// Legal is the operator's legal identity from config.Config.Legal,
	// rendered by the footer and the Terms/Privacy pages.
	Legal webmiddleware.LegalIdentity
```

- After `router.Use(webmiddleware.Instance(dependencies.InstanceName))`:

```go
	router.Use(webmiddleware.Legal(dependencies.Legal))
```

- After `navigation := handler.NewNavigation(...)`:

```go
	legalHandler := handler.NewLegal(navigation, brand)
```

- Inside the `authenticated` group, after `authenticated.Get("/", rootHandler(...))` (public: no `RequireAuthentication`, but the session is resolved so signed-in players get their shell):

```go
		authenticated.Get("/terms", legalHandler.Terms)
		authenticated.Get("/privacy", legalHandler.Privacy)
```

`internal/app/app.go`, in the `web.RouterDependencies{…}` literal after `InstanceName`:

```go
			Legal: webmiddleware.LegalIdentity{
				EntityName: cfg.Legal.EntityName, Address: cfg.Legal.Address,
				ContactEmail: cfg.Legal.ContactEmail, ImprintURL: cfg.Legal.ImprintURL,
			},
```

importing `webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"` if not already imported.

Regenerate: `go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate`, then `npm run build` (the page uses new Tailwind arbitrary variants).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/web/... ./internal/app/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/pages/legal.templ internal/web/pages/legal_templ.go internal/web/handler/legal.go internal/web/router.go internal/web/router_test.go internal/app/app.go internal/web/assets/dist
git commit -m "feat(web): add public terms of service and privacy policy pages"
```

---

### Task 5: Configuration examples and canonical docs

**Files:**
- Modify: `.env.example`, `compose.yaml`, `README.md`, `docs/PRODUCT.md` (§14), `docs/ARCHITECTURE.md` (§15, §16), `docs/superpowers/README.md`

- [ ] **Step 1: `.env.example`** — append:

```dotenv
# Operator legal identity shown on /terms and /privacy. Required when Discord
# login is configured (entity name and contact email). Keep the address on one
# line; separate parts with commas.
LANSURFER_LEGAL_ENTITY_NAME=Computerfreunde Karlsdorf-Neuthard e.V.
LANSURFER_LEGAL_ADDRESS=<Street and number>, <Postal code> Karlsdorf-Neuthard, Germany
LANSURFER_LEGAL_CONTACT_EMAIL=datenschutz@convention-x-treme.de
# Optional absolute http(s) link to an external imprint (Impressum), e.g.
# https://www.convention-x-treme.de/impressum (placeholder: confirm the URL).
LANSURFER_IMPRINT_URL=
```

The imprint stays empty here because a `<…>` placeholder would fail URL validation at startup.

- [ ] **Step 2: `compose.yaml`** — after `LANSURFER_INSTANCE_NAME`:

```yaml
      LANSURFER_LEGAL_ENTITY_NAME: ${LANSURFER_LEGAL_ENTITY_NAME:-}
      LANSURFER_LEGAL_ADDRESS: ${LANSURFER_LEGAL_ADDRESS:-}
      LANSURFER_LEGAL_CONTACT_EMAIL: ${LANSURFER_LEGAL_CONTACT_EMAIL:-}
      LANSURFER_IMPRINT_URL: ${LANSURFER_IMPRINT_URL:-}
```

- [ ] **Step 3: `README.md`** — after the "Configure minimum instance identity" section add:

````markdown
### Configure legal identity

```sh
LANSURFER_LEGAL_ENTITY_NAME=
LANSURFER_LEGAL_ADDRESS=
LANSURFER_LEGAL_CONTACT_EMAIL=
LANSURFER_IMPRINT_URL=
```

LANsurfer serves a public Terms of Service at `/terms` and Privacy Policy at `/privacy`, linked from the footer of every screen. These values fill in the operator (data controller) on both pages; unset values show as "not configured" and the imprint link is hidden. Entity name and address are single-line (200 and 300 characters), the contact email must be a bare address, and the imprint must be an absolute `http(s)` URL. **Discord login requires the entity name and contact email**; LANsurfer refuses to start without them, because Discord requires a privacy policy with a responsible contact. Register `<base URL>/terms` and `<base URL>/privacy` in the Discord Developer Portal. The page text describes the reference deployment (30-day log retention, self-operated server in a colocation data center); another operator edits `internal/web/pages/legal.templ` to match theirs and has both texts legally reviewed.
````

- [ ] **Step 4: `docs/PRODUCT.md` §14** — after the paragraph starting "Stable 1.0 does **not** require a full administrator-managed branding surface", add:

```markdown
Operator legal identity is the one configurable exception: legal entity name, postal address, contact email, and an optional external imprint URL are operator configuration, because each deployment is its own data controller. LANsurfer serves a public Terms of Service (`/terms`) and Privacy Policy (`/privacy`) filled from these values, links them together with a contact/reporting entry and the imprint from the footer of every screen, and states on the login screen that logging in accepts the Terms. Support, abuse, Discord, and data requests go to the contact email; software bugs go to the project's issue tracker. This does not reintroduce welcome text, logos, colors, or free-form legal text as configuration.
```

- [ ] **Step 5: `docs/ARCHITECTURE.md`** — in §15 add the four variables to the example list after `LANSURFER_COVER_CACHE_DIR`, and after the "Minimum instance identity" paragraph add:

```markdown
Operator legal identity (`LANSURFER_LEGAL_ENTITY_NAME`, `LANSURFER_LEGAL_ADDRESS`, `LANSURFER_LEGAL_CONTACT_EMAIL`, `LANSURFER_IMPRINT_URL`) is the one contact-bearing exception: it fills the fixed Terms/Privacy text and footer, travels to templates through request-context middleware like the instance name, and is required (entity name and contact email) whenever Discord login is configured.
```

In §16, change "A deployment may expose only the minimum instance identity required to distinguish the installation." to "A deployment may expose only the minimum instance identity required to distinguish the installation, plus the operator legal identity described in §15."

- [ ] **Step 6: `docs/superpowers/README.md`** — before the final paragraph add:

```markdown
## Legal pages

1. `legal/2026-09-22-legal-pages-design.md` — public Terms, Privacy, and contact/reporting surface for issue #39 section 1.
2. `legal/2026-09-22-legal-pages-plan.md` — execution checklist for that design.
```

- [ ] **Step 7: Full verification**

Run: `task lint && task test && task build`
Expected: all succeed; `git status` shows no uncommitted generated changes after `task frontend:build`.

- [ ] **Step 8: Commit**

```bash
git add .env.example compose.yaml README.md docs/PRODUCT.md docs/ARCHITECTURE.md docs/superpowers/README.md
git commit -m "docs: document operator legal identity and legal pages"
```
