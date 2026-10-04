package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestGameRequestSessionInformationAudienceAndForm(t *testing.T) {
	entry := gamerequest.SessionInformationEntry{ID: "entry-1", Key: "Server", Value: "address"}
	for _, test := range []struct {
		name          string
		view          SessionInformationView
		want, notWant []string
	}{
		{"participant", SessionInformationView{Visible: true, RequestID: "request-1", Entries: []gamerequest.SessionInformationEntry{entry}}, []string{"Server", "address"}, []string{"Add", "Edit", "Delete"}},
		{"host", SessionInformationView{Visible: true, CanEdit: true, RequestID: "request-1", Entries: []gamerequest.SessionInformationEntry{entry}}, []string{"Add", "Edit Server", "Delete Server"}, nil},
		{"outsider", SessionInformationView{Visible: true, JoinRequired: true, RequestID: "request-1", Entries: []gamerequest.SessionInformationEntry{entry}}, []string{"Join this Game Request to see session information."}, []string{"Server", "address"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := GameRequestSessionInformation(test.view).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			for _, s := range test.want {
				if !strings.Contains(out.String(), s) {
					t.Errorf("missing %q", s)
				}
			}
			for _, s := range test.notWant {
				if strings.Contains(out.String(), s) {
					t.Errorf("unexpected %q", s)
				}
			}
		})
	}
}
func TestSessionInformationUsesCompactContentTilesAndInlineEditor(t *testing.T) {
	entry := gamerequest.SessionInformationEntry{ID: "entry-1", Key: "Server", Value: strings.Repeat("10.20.30.40:", 4)}
	view := SessionInformationView{
		Visible: true, CanEdit: true, RequestID: "request-1",
		Entries: []gamerequest.SessionInformationEntry{entry},
		Form:    &SessionInformationForm{Mode: "edit", EntryID: entry.ID, Key: entry.Key, Value: entry.Value},
	}
	var out bytes.Buffer
	if err := GameRequestSessionInformation(view).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		`id="session-information"`,
		`class="session-information-tiles"`,
		`class="session-information-tile"`,
		`class="session-information-form"`,
		`class="session-information-actions"`,
		`aria-label="Edit Server"`,
		`aria-label="Delete Server"`,
		`name="key"`,
		`name="value"`,
		"Save",
		"Cancel",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("session information markup missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, `class="detail-card`) {
		t.Fatalf("session information must not add a nested card: %s", html)
	}
}

func TestSecretSessionInformationIsMaskedAndEditableAsPassword(t *testing.T) {
	entry := gamerequest.SessionInformationEntry{ID: "entry-1", Key: "Password", Value: "hunter2", IsSecret: true}
	view := SessionInformationView{
		Visible: true, CanEdit: true, RequestID: "request-1", Entries: []gamerequest.SessionInformationEntry{entry},
		Form: &SessionInformationForm{Mode: "edit", EntryID: entry.ID, Key: entry.Key, Value: entry.Value, IsSecret: entry.IsSecret},
	}
	var out bytes.Buffer
	if err := GameRequestSessionInformation(view).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{`type="password"`, `name="is_secret"`, `checked`, "Secret", "Copy", `data-secret-value`} {
		if !strings.Contains(html, want) {
			t.Errorf("secret session information markup missing %q: %s", want, html)
		}
	}
}

func TestNormalSessionInformationFormLeavesSecretToggleUnchecked(t *testing.T) {
	view := SessionInformationView{Visible: true, CanEdit: true, Form: &SessionInformationForm{Mode: "add"}}
	var out bytes.Buffer
	if err := GameRequestSessionInformation(view).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `name="is_secret" value="true" checked`) {
		t.Fatalf("normal entry form checks secret toggle: %s", out.String())
	}
}

func TestSessionInformationFormUsesServerUnicodeLimitsAndMarksRejectedField(t *testing.T) {
	view := SessionInformationView{
		Visible: true, CanEdit: true, RequestID: "request-1",
		Form: &SessionInformationForm{Mode: "add", Key: strings.Repeat("😀", 40), Value: strings.Repeat("😀", 300), Error: "Key must contain 1 to 40 characters and no line breaks.", ErrorField: "key"},
	}
	var out bytes.Buffer
	if err := GameRequestSessionInformation(view).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	if strings.Contains(html, "maxlength=") || !strings.Contains(html, `aria-invalid="true"`) ||
		!strings.Contains(html, `aria-describedby="session-information-error"`) ||
		!strings.Contains(html, `id="session-information-error"`) {
		t.Fatalf("session information form markup = %s", html)
	}
}
func TestSessionInformationEmptyAndOutsiderStatesStayCompact(t *testing.T) {
	for _, test := range []struct {
		name    string
		view    SessionInformationView
		want    []string
		notWant []string
	}{
		{
			name: "host empty",
			view: SessionInformationView{Visible: true, CanEdit: true, RequestID: "request-1"},
			want: []string{"No session information yet.", "Add"},
		},
		{
			name:    "participant empty",
			view:    SessionInformationView{Visible: true, RequestID: "request-1"},
			want:    []string{"No session information yet."},
			notWant: []string{"Add", "Edit", "Delete"},
		},
		{
			name: "outsider",
			view: SessionInformationView{
				Visible: true, JoinRequired: true, CanEdit: false, RequestID: "request-1",
				Entries: []gamerequest.SessionInformationEntry{{ID: "entry-1", Key: "Secret", Value: "hidden"}},
			},
			want:    []string{"Join this Game Request to see session information."},
			notWant: []string{"Secret", "hidden", "Add", "Edit", "Delete"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := GameRequestSessionInformation(test.view).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			for _, required := range test.want {
				if !strings.Contains(html, required) {
					t.Errorf("missing %q: %s", required, html)
				}
			}
			for _, unwanted := range test.notWant {
				if strings.Contains(html, unwanted) {
					t.Errorf("unexpected %q: %s", unwanted, html)
				}
			}
			if strings.Contains(html, `class="detail-card`) {
				t.Errorf("session information must not add a nested card: %s", html)
			}
		})
	}
}
