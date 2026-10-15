package pim

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func sample() Group {
	g := Group{GroupID: "group-123", AccessID: "Access-ID", PrincipalID: "principal-456"}
	g.Group = &struct {
		DisplayName string `json:"displayName"`
	}{"Engineering"}
	return g
}

func TestKeyCaseInsensitive(t *testing.T) {
	g := sample()
	if Key(g) != "group-123|access-id" {
		t.Fatal(Key(g))
	}
	g2 := g
	g2.AccessID = "ACCESS-ID"
	if Key(g) != Key(g2) {
		t.Fatal("keys differ")
	}
}

func TestName(t *testing.T) {
	g := sample()
	if Name(g) != "Engineering" {
		t.Fatal(Name(g))
	}
	g.Group = nil
	if Name(g) != "group-123" {
		t.Fatal(Name(g))
	}
}

func TestBuildActivationRequestJSON(t *testing.T) {
	b, _ := json.Marshal(BuildActivationRequest(sample(), 2.5, "On-call"))
	want := `{"action":"SelfActivate","directoryScopeId":null,"principalId":"principal-456","accessId":"Access-ID","groupId":"group-123","isValidationOnly":false,"justification":"On-call","scheduleInfo":{"startDateTime":null,"expiration":{"type":"AfterDuration","duration":"PT150M"}},"ticketInfo":{"ticketNumber":"","ticketSystem":""}}`
	if string(b) != want {
		t.Fatalf("got %s", b)
	}
}

func TestDurationRounds(t *testing.T) {
	if d := BuildActivationRequest(sample(), 0.501, "x").ScheduleInfo.Expiration.Duration; d != "PT30M" {
		t.Fatal(d)
	}
}

func TestActiveNote(t *testing.T) {
	if n := ActiveNote(Assignment{AssignmentType: "Activated"}); n != "ACTIVE until permanent" {
		t.Fatal(n)
	}
	if n := ActiveNote(Assignment{AssignmentType: "Assigned"}); n != "ASSIGNED (permanent)" {
		t.Fatal(n)
	}
}

func TestAccountFromToken(t *testing.T) {
	tok := func(payload string) string {
		return "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
	}
	if got := AccountFromToken(tok(`{"name":"Kim","upn":"kim@x.com"}`)); got != "Kim (kim@x.com)" {
		t.Fatal(got)
	}
	if got := AccountFromToken(tok(`{"unique_name":"a@b.c"}`)); got != "a@b.c" {
		t.Fatal(got)
	}
	for _, bad := range []string{"", "abc", "a.!!.c", tok("not json")} {
		if got := AccountFromToken(bad); got != "" {
			t.Fatal(bad, got)
		}
	}
}

func TestSplitPath(t *testing.T) {
	got := SplitPath("sec-azure-sub-ebcdev-keyvaultAdministrator")
	if len(got) != 5 || got[3] != "ebcdev" || got[4] != "keyvaultAdministrator" {
		t.Fatal(got)
	}
	if got := SplitPath("plain"); len(got) != 1 || got[0] != "plain" {
		t.Fatal(got)
	}
	if got := SplitPath("---"); len(got) != 1 || got[0] != "---" {
		t.Fatal(got)
	}
}
