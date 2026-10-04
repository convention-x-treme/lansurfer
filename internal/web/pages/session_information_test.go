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
		{"host", SessionInformationView{Visible: true, CanEdit: true, RequestID: "request-1", Entries: []gamerequest.SessionInformationEntry{entry}}, []string{"Add", "Edit"}, nil},
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
