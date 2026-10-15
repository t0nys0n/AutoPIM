// Package pim holds the PIM-for-Groups domain logic.
package pim

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"autopim/internal/graph"
)

type Group struct {
	GroupID     string `json:"groupId"`
	AccessID    string `json:"accessId"`
	PrincipalID string `json:"principalId"`
	Group       *struct {
		DisplayName string `json:"displayName"`
	} `json:"group"`
}

type Assignment struct {
	Group
	AssignmentType string `json:"assignmentType"`
	EndDateTime    string `json:"endDateTime"`
}

type State struct {
	All    []Group
	Active map[string]Assignment
}

type Expiration struct {
	Type     string `json:"type"`
	Duration string `json:"duration"`
}

type ScheduleInfo struct {
	StartDateTime *string    `json:"startDateTime"`
	Expiration    Expiration `json:"expiration"`
}

type TicketInfo struct {
	TicketNumber string `json:"ticketNumber"`
	TicketSystem string `json:"ticketSystem"`
}

type ActivationRequest struct {
	Action           string       `json:"action"`
	DirectoryScopeID *string      `json:"directoryScopeId"`
	PrincipalID      string       `json:"principalId"`
	AccessID         string       `json:"accessId"`
	GroupID          string       `json:"groupId"`
	IsValidationOnly bool         `json:"isValidationOnly"`
	Justification    string       `json:"justification"`
	ScheduleInfo     ScheduleInfo `json:"scheduleInfo"`
	TicketInfo       TicketInfo   `json:"ticketInfo"`
}

// TokenExpr runs in the portal page and returns the longest-lived Graph token
// that has the PIM group write scope, or null.
const TokenExpr = `(() => {
  const now = Date.now() / 1000;
  let best = null;
  for (const k of Object.keys(sessionStorage)) {
    if (!/accesstoken/i.test(k) || !k.includes("graph.microsoft.com")) continue;
    let o;
    try { o = JSON.parse(sessionStorage.getItem(k)); } catch { continue; }
    if (!String(o.target || "").toLowerCase().includes("privilegedassignmentschedule.readwrite.azureadgroup")) continue;
    const exp = Number(o.expiresOn);
    if (!o.secret || exp < now + 60) continue;
    if (!best || exp > best.exp) best = { exp, secret: o.secret };
  }
  return best ? best.secret : null;
})()`

func Key(g Group) string { return g.GroupID + "|" + strings.ToLower(g.AccessID) }

func Name(g Group) string {
	if g.Group != nil && g.Group.DisplayName != "" {
		return g.Group.DisplayName
	}
	return g.GroupID
}

func FmtTime(iso string) string {
	if iso == "" {
		return "permanent"
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.Local().Format("2006-01-02 15:04")
}

func ActiveNote(a Assignment) string {
	if strings.EqualFold(a.AssignmentType, "activated") {
		return "ACTIVE until " + FmtTime(a.EndDateTime)
	}
	return "ASSIGNED (" + FmtTime(a.EndDateTime) + ")"
}

func LoadState(token string) (State, error) {
	var (
		wg          sync.WaitGroup
		eligible    []Group
		assignments []Assignment
		e1, e2      error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		eligible, e1 = graph.List[Group](token, "/eligibilityScheduleInstances/filterByCurrentUser(on='principal')?$expand=group")
	}()
	go func() {
		defer wg.Done()
		assignments, e2 = graph.List[Assignment](token, "/assignmentScheduleInstances/filterByCurrentUser(on='principal')")
	}()
	wg.Wait()
	if e1 != nil {
		return State{}, e1
	}
	if e2 != nil {
		return State{}, e2
	}
	active := make(map[string]Assignment, len(assignments))
	for _, a := range assignments {
		active[Key(a.Group)] = a
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return strings.ToLower(Name(eligible[i])) < strings.ToLower(Name(eligible[j]))
	})
	return State{All: eligible, Active: active}, nil
}

func BuildActivationRequest(g Group, hours float64, reason string) ActivationRequest {
	return ActivationRequest{
		Action:        "SelfActivate",
		PrincipalID:   g.PrincipalID,
		AccessID:      g.AccessID,
		GroupID:       g.GroupID,
		Justification: reason,
		ScheduleInfo: ScheduleInfo{
			Expiration: Expiration{Type: "AfterDuration", Duration: fmt.Sprintf("PT%dM", int(math.Round(hours*60)))},
		},
	}
}

// Activate submits an activation request and returns a short status text and success flag.
func Activate(token string, g Group, hours float64, reason string) (string, bool) {
	res, err := graph.Do(token, http.MethodPost, "/assignmentScheduleRequests", BuildActivationRequest(g, hours, reason))
	if err != nil {
		return "FAILED: " + err.Error(), false
	}
	if !res.OK() {
		return fmt.Sprintf("FAILED %d: %s", res.Status, graph.ErrorMessage(res.Body)), false
	}
	if s := graph.Status(res.Body); s != "" {
		return s, true
	}
	return "submitted", true
}

// AccountFromToken returns a "Name (user@domain)" label read from the token's
// claims. The token is only decoded locally, never printed. Returns "" if unknown.
func AccountFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var c struct {
		Name              string `json:"name"`
		UPN               string `json:"upn"`
		UniqueName        string `json:"unique_name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	id := c.UPN
	if id == "" {
		id = c.UniqueName
	}
	if id == "" {
		id = c.PreferredUsername
	}
	switch {
	case c.Name != "" && id != "":
		return c.Name + " (" + id + ")"
	case id != "":
		return id
	}
	return c.Name
}

// SplitPath splits a group name into "-" separated segments for tree grouping.
// A name without usable segments is returned as a single segment.
func SplitPath(name string) []string {
	segs := strings.FieldsFunc(name, func(r rune) bool { return r == '-' })
	if len(segs) == 0 {
		return []string{name}
	}
	return segs
}
