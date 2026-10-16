package chrome

import (
	"strings"
	"testing"
)

func winEnv(k string) string {
	return map[string]string{
		"PROGRAMFILES":      `C:\Program Files`,
		"PROGRAMFILES(X86)": `C:\Program Files (x86)`,
		"LOCALAPPDATA":      `C:\Users\me\AppData\Local`,
	}[k]
}

func TestWindowsCandidates(t *testing.T) {
	if n := len(Candidates("windows", winEnv)); n != 6 {
		t.Fatalf("got %d", n)
	}
	if n := len(Candidates("windows", func(string) string { return "" })); n != 0 {
		t.Fatalf("got %d", n)
	}
}

func TestFind(t *testing.T) {
	list := Candidates("windows", winEnv)
	got, err := Find("windows", winEnv, func(p string) bool { return p == list[3] })
	if err != nil || got != list[3] {
		t.Fatal(got, err)
	}
	if _, err := Find("linux", winEnv, func(string) bool { return false }); err == nil {
		t.Fatal("expected error")
	}
}

func TestPlatformPaths(t *testing.T) {
	for _, p := range Candidates("linux", winEnv) {
		if !strings.HasPrefix(p, "/usr/bin/") {
			t.Fatal(p)
		}
	}
	for _, p := range Candidates("darwin", winEnv) {
		if !strings.HasPrefix(p, "/Applications/") {
			t.Fatal(p)
		}
	}
}

func TestIsOurCommand(t *testing.T) {
	if !IsOurCommand("chrome --user-data-dir=/p --x", "/p") || IsOurCommand("chrome --user-data-dir=/q", "/p") {
		t.Fatal("mismatch")
	}
}
