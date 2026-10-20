// Package chrome launches Chrome with remote debugging and talks CDP to it.
package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	Port      = "9222"
	BaseURL   = "http://127.0.0.1:" + Port
	PortalURL = "https://portal.azure.com"
	StartURL  = "https://portal.azure.com/#view/Microsoft_Azure_PIMCommon/ActivationMenuBlade/~/aadgroup"
	WaitLimit = 5 * time.Minute
)

var (
	profile     = func() string { h, _ := os.UserHomeDir(); return filepath.Join(h, ".pim-chrome") }()
	launched    bool
	httpClient  = &http.Client{Timeout: 5 * time.Second}
	ErrNoTarget = errors.New("no target")
)

type Target struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func listTargets() ([]Target, error) {
	res, err := httpClient.Get(BaseURL + "/json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var t []Target
	return t, json.NewDecoder(res.Body).Decode(&t)
}

// send runs one CDP command on a fresh connection and returns its result.
func send(wsURL, method string, params map[string]any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", wsURL, err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := conn.WriteJSON(map[string]any{"id": 1, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		var msg struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return nil, err
		}
		if msg.ID != 1 {
			continue
		}
		if len(msg.Error) > 0 {
			return nil, errors.New(string(msg.Error))
		}
		return msg.Result, nil
	}
}

// EvalString evaluates an expression in the page and returns a string result ("" for null).
func EvalString(wsURL, expr string) (string, error) {
	raw, err := send(wsURL, "Runtime.evaluate", map[string]any{
		"expression": expr, "returnByValue": true, "awaitPromise": true,
	})
	if err != nil {
		return "", err
	}
	var r struct {
		Result struct {
			Value *string `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", err
	}
	if len(r.ExceptionDetails) > 0 {
		return "", errors.New(string(r.ExceptionDetails))
	}
	if r.Result.Value == nil {
		return "", nil
	}
	return *r.Result.Value, nil
}

// Ensure starts Chrome with the debugging port unless it is already listening.
func Ensure() error {
	if _, err := listTargets(); err == nil {
		if !ownsPort() {
			fmt.Fprintf(os.Stderr, "Warning: port %s is already served by a browser not started by autopim; connecting to it.\n", Port)
		}
		return nil
	}
	fmt.Println("Launching browser...")
	bin, err := Find(runtime.GOOS, os.Getenv, fileExists)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin,
		"--remote-debugging-port="+Port,
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir="+profile,
		"--no-first-run", StartURL)
	if err := cmd.Start(); err != nil {
		return err
	}
	launched = true
	go cmd.Wait()
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		if _, err := listTargets(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("browser did not open debugging port %s", Port)
}

// ownsPort detects a leftover Chrome from an earlier run that used our profile.
func ownsPort() bool {
	var out []byte
	var err error
	if runtime.GOOS == "windows" {
		out, err = exec.Command("powershell", "-NoProfile", "-Command",
			"(Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*--remote-debugging-port="+Port+"*' }).CommandLine").Output()
	} else {
		var pid []byte
		pid, err = exec.Command("lsof", "-ti", "tcp:"+Port, "-sTCP:LISTEN").Output()
		if err == nil {
			first := strings.Fields(string(pid))
			if len(first) == 0 {
				return false
			}
			out, err = exec.Command("ps", "-o", "command=", "-p", first[0]).Output()
		}
	}
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(out), "\n") {
		if IsOurCommand(l, profile) {
			return true
		}
	}
	return false
}

// Close shuts down Chrome if we started it (now or in an earlier run).
func Close() {
	if !launched && !ownsPort() {
		return
	}
	launched = false
	res, err := httpClient.Get(BaseURL + "/json/version")
	if err != nil {
		return
	}
	defer res.Body.Close()
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if json.NewDecoder(res.Body).Decode(&v) != nil || v.WS == "" {
		return
	}
	conn, _, err := websocket.DefaultDialer.Dial(v.WS, nil)
	if err != nil {
		return
	}
	conn.WriteJSON(map[string]any{"id": 1, "method": "Browser.close"})
	time.Sleep(500 * time.Millisecond)
	conn.Close()
}

// Waiter waits for a signed-in portal page, sharing one overall deadline.
type Waiter struct {
	deadline                  time.Time
	opened, navigated, notice bool
}

func NewWaiter() *Waiter { return &Waiter{deadline: time.Now().Add(WaitLimit)} }

// WaitForToken polls the portal page until expr yields a non-empty value.
// It returns the page's debugger URL and the value.
func (w *Waiter) WaitForToken(expr string) (string, string, error) {
	for {
		if time.Now().After(w.deadline) {
			return "", "", errors.New("Timed out waiting for sign-in / page content.")
		}
		targets, _ := listTargets()
		hasPage := false
		for _, t := range targets {
			if t.Type == "page" {
				hasPage = true
			}
		}
		if !hasPage && !w.opened {
			req, _ := http.NewRequest(http.MethodPut, BaseURL+"/json/new?"+url.QueryEscape(StartURL), nil)
			if r, err := httpClient.Do(req); err == nil {
				r.Body.Close()
			}
			w.opened = true
		}
		for _, t := range targets {
			if t.Type != "page" || !strings.HasPrefix(t.URL, PortalURL) {
				continue
			}
			if !w.navigated && !strings.HasPrefix(t.URL, StartURL) {
				send(t.WebSocketDebuggerURL, "Page.navigate", map[string]any{"url": StartURL})
				w.navigated = true
				time.Sleep(time.Second)
				break
			}
			w.navigated = true
			if v, err := EvalString(t.WebSocketDebuggerURL, expr); err == nil && v != "" {
				return t.WebSocketDebuggerURL, v, nil
			}
			break
		}
		if !w.notice {
			fmt.Println("Waiting for sign-in. Please sign in to Azure in the browser window... (Ctrl+C to cancel)")
			w.notice = true
		}
		time.Sleep(time.Second)
	}
}
