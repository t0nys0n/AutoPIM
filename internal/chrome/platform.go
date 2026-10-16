package chrome

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Candidates lists browser executables to try for the given GOOS.
func Candidates(goos string, env func(string) string) []string {
	switch goos {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		var out []string
		for _, k := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			root := env(k)
			if root == "" {
				continue
			}
			out = append(out,
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
		return out
	}
	return []string{
		"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable",
		"/usr/bin/chromium", "/usr/bin/chromium-browser", "/usr/bin/microsoft-edge",
	}
}

func Find(goos string, env func(string) string, exists func(string) bool) (string, error) {
	for _, c := range Candidates(goos, env) {
		if exists(c) {
			return c, nil
		}
	}
	return "", errors.New("Chrome or Edge not found.")
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// IsOurCommand reports whether a command line was started with our profile.
func IsOurCommand(command, profile string) bool {
	return strings.Contains(command, "--user-data-dir="+profile)
}
