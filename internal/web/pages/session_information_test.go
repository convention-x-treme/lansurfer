package pages

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestGameRequestSessionInformationAudienceAndForm(t *testing.T) {
	entry := gamerequest.SessionInformationEntry{ID:"entry-1",Key:"Server",Value:"address"}
	for _, test := range []struct{name string; view SessionInformationView; want, notWant []string}{
		{"participant",SessionInformationView{Visible:true,RequestID:"request-1",Entries:[]gamerequest.SessionInformationEntry{entry}},[]string{"Server","address"},[]string{"Add","Edit","Delete"}},
		{"host",SessionInformationView{Visible:true,CanEdit:true,RequestID:"request-1",Entries:[]gamerequest.SessionInformationEntry{entry}},[]string{"Add","Edit"},nil},
		{"outsider",SessionInformationView{Visible:true,JoinRequired:true,RequestID:"request-1",Entries:[]gamerequest.SessionInformationEntry{entry}},[]string{"Join this Game Request to see session information."},[]string{"Server","address"}},
	} { t.Run(test.name,func(t *testing.T){ var out bytes.Buffer; if err:=GameRequestSessionInformation(test.view).Render(context.Background(),&out); err!=nil {t.Fatal(err)}; for _,s:=range test.want {if !strings.Contains(out.String(),s){t.Errorf("missing %q",s)}}; for _,s:=range test.notWant {if strings.Contains(out.String(),s){t.Errorf("unexpected %q",s)}} }) }
}
