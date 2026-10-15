// Package graph is a minimal Microsoft Graph client for PIM group APIs.
package graph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const BaseURL = "https://graph.microsoft.com/v1.0/identityGovernance/privilegedAccess/group"

var client = &http.Client{Timeout: 30 * time.Second}

var spaces = regexp.MustCompile(`\s+`)

// Response is a Graph reply; Body is decoded JSON, or the raw text when it is not JSON.
type Response struct {
	Status int
	Body   any
}

func (r Response) OK() bool { return r.Status >= 200 && r.Status < 300 }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// Status returns the "status" string of a response body, or "" if absent.
func Status(body any) string {
	if m, ok := body.(map[string]any); ok {
		if s, ok := m["status"].(string); ok {
			return s
		}
	}
	return ""
}

// ErrorMessage extracts a short, single-line error message from a response body.
func ErrorMessage(body any) string {
	if m, ok := body.(map[string]any); ok {
		if e, ok := m["error"].(map[string]any); ok {
			if msg, ok := e["message"].(string); ok {
				return truncate(strings.TrimSpace(spaces.ReplaceAllString(msg, " ")), 120)
			}
		}
	}
	b, _ := json.Marshal(body)
	return truncate(spaces.ReplaceAllString(string(b), " "), 120)
}

// resolveURL maps a relative path under BaseURL, and only accepts absolute URLs
// (e.g. a nextLink) on the Graph host, so the bearer token is never sent elsewhere.
func resolveURL(path string) (string, error) {
	if !strings.HasPrefix(path, "http") {
		return BaseURL + path, nil
	}
	u, err := url.Parse(path)
	if err != nil || u.Scheme != "https" || u.Hostname() != "graph.microsoft.com" {
		return "", fmt.Errorf("refusing to send token to non-Graph URL")
	}
	return path, nil
}

// Do sends a request to a path under BaseURL, or to an absolute URL (e.g. a nextLink).
func Do(token, method, path string, payload any) (Response, error) {
	target, err := resolveURL(path)
	if err != nil {
		return Response{}, err
	}
	var rd io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Response{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, target, rd)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return Response{}, err
	}
	var body any = string(data)
	if len(bytes.TrimSpace(data)) == 0 {
		body = nil
	} else {
		var v any
		if json.Unmarshal(data, &v) == nil {
			body = v
		}
	}
	return Response{Status: res.StatusCode, Body: body}, nil
}

// List follows @odata.nextLink and collects every page's "value" items.
func List[T any](token, path string) ([]T, error) {
	var out []T
	next := path
	for next != "" {
		res, err := Do(token, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		if res.Status != http.StatusOK {
			return nil, fmt.Errorf("Graph %d: %s", res.Status, ErrorMessage(res.Body))
		}
		raw, _ := json.Marshal(res.Body)
		var page struct {
			Value    []T    `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if _, ok := res.Body.(map[string]any); !ok || json.Unmarshal(raw, &page) != nil {
			return nil, fmt.Errorf("Graph returned an invalid response")
		}
		out = append(out, page.Value...)
		next = page.NextLink
	}
	return out, nil
}
