package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"autopim/internal/chrome"
	"autopim/internal/pim"
	"autopim/internal/ui"
)

func main() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		chrome.Close()
		os.Exit(130)
	}()

	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	chrome.Close()
	if err != nil {
		os.Exit(1)
	}
}

func prompt(r *bufio.Reader, msg string) (string, error) {
	fmt.Print(msg)
	s, err := r.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func run() error {
	if err := chrome.Ensure(); err != nil {
		return err
	}
	pageWS, token, err := chrome.NewWaiter().WaitForToken(pim.TokenExpr)
	if err != nil {
		return err
	}
	readToken := func() string {
		if t, err := chrome.EvalString(pageWS, pim.TokenExpr); err == nil && t != "" {
			return t
		}
		return token
	}

	lastStatus := map[string]string{}
	hours := 0.0
	reason := ""

	for {
		tk := readToken()
		st, err := pim.LoadState(tk)
		if err != nil {
			return err
		}
		var selectable, activeCount int
		for _, g := range st.All {
			if _, ok := st.Active[pim.Key(g)]; ok {
				activeCount++
			} else {
				selectable++
			}
		}
		if selectable == 0 {
			fmt.Printf("\n\x1b[1m\x1b[36mAzure PIM\x1b[0m \x1b[1m\u203a Privileged access Groups\x1b[0m  \x1b[90m\u2014 %d/%d active\x1b[0m\n", len(st.All), len(st.All))
			if a := pim.AccountFromToken(tk); a != "" {
				fmt.Printf("\x1b[90mAccount:\x1b[0m \x1b[1m%s\x1b[0m\n", a)
			}
			for _, g := range st.All {
				fmt.Printf("  \x1b[32m\u2714\x1b[0m %-55s %s\n", pim.Name(g), pim.ActiveNote(st.Active[pim.Key(g)]))
			}
			fmt.Println("\nAll groups are already active.")
			return nil
		}

		items := make([]ui.Choice, len(st.All))
		for i, g := range st.All {
			if a, ok := st.Active[pim.Key(g)]; ok {
				items[i] = ui.Choice{Disabled: true, Note: pim.ActiveNote(a)}
			} else {
				items[i] = ui.Choice{Note: lastStatus[pim.Key(g)]}
			}
			items[i].Label = pim.Name(g)
			items[i].Path = pim.SplitPath(items[i].Label)
		}
		account := "unknown"
		if a := pim.AccountFromToken(tk); a != "" {
			account = a
		}
		title := fmt.Sprintf("\n\x1b[1m\x1b[36mAzure PIM\x1b[0m \x1b[1m\u203a Privileged access Groups\x1b[0m  \x1b[90m\u2014 My roles\x1b[0m\n"+
			"\x1b[90mAccount:\x1b[0m \x1b[1m%s\x1b[0m\n"+
			"\x1b[90mEligible assignments:\x1b[0m %d   \x1b[32mActive:\x1b[0m %d   \x1b[33mActivatable:\x1b[0m %d",
			account, len(st.All), activeCount, selectable)
		picked, err := ui.Select(title, items)
		if err != nil {
			return err
		}
		if len(picked) == 0 {
			fmt.Println("Done.")
			return nil
		}

		in := bufio.NewReader(os.Stdin)
		for !(hours >= 0.5 && hours <= 24) {
			s, err := prompt(in, "Duration in hours (0.5-24) [8]: ")
			if err != nil {
				return errors.New("input closed")
			}
			if s == "" {
				s = "8"
			}
			hours, _ = strconv.ParseFloat(s, 64)
		}
		for reason == "" || len([]rune(reason)) > 500 {
			if reason, err = prompt(in, "Reason (max 500 chars): "); err != nil {
				return errors.New("input closed")
			}
		}

		fmt.Printf("\nSending %d request(s) in parallel (%gh)...\n", len(picked), hours)
		var (
			mu       sync.Mutex
			wg       sync.WaitGroup
			accepted []pim.Group
		)
		for _, i := range picked {
			g := st.All[i]
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, ok := pim.Activate(tk, g, hours, reason)
				mu.Lock()
				defer mu.Unlock()
				lastStatus[pim.Key(g)] = status
				mark := "\u2718"
				if ok {
					accepted = append(accepted, g)
					mark = "\u2714"
				}
				fmt.Printf("  %s %s  %s\n", mark, pim.Name(g), status)
			}()
		}
		wg.Wait()

		// Wait until every accepted request shows up as active (or give up).
		waiting := map[string]bool{}
		for _, g := range accepted {
			waiting[pim.Key(g)] = true
		}
		if len(waiting) > 0 {
			fmt.Print("\nChecking status")
		}
		for i := 0; i < 10 && len(waiting) > 0; i++ {
			time.Sleep(2 * time.Second)
			fmt.Print(".")
			if cur, err := pim.LoadState(readToken()); err == nil {
				for k := range waiting {
					if _, ok := cur.Active[k]; ok {
						delete(waiting, k)
					}
				}
			}
		}
		if len(accepted) > 0 {
			fmt.Println()
		}
	}
}
