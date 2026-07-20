package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"claude-quota/internal/badge"
	"claude-quota/internal/updatecheck"
)

// padRight pads s to at least width Unicode code points using spaces.
// fmt.Sprintf("%-Ns") pads by bytes, which breaks for multi-byte runes (e.g. █).
func padRight(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

func meter(u float64) string {
	filled := int(math.Min(10, math.Max(0, math.Round(u/10))))
	return strings.Repeat("█", filled)
}

func resetStr(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	local := t.Local()
	now := time.Now()
	day := "today"
	if local.Year() != now.Year() || local.YearDay() != now.YearDay() {
		day = local.Format("Mon")
	}
	return fmt.Sprintf("resets %s %s", day, local.Format("15:04"))
}

func windowLine(label string, w *Window) string {
	return windowLineFields(label, w.Utilization, w.ResetsAt)
}

func windowLineFields(label string, u float64, resetsAt string) string {
	return fmt.Sprintf("%s %s %3.0f%%  %s | font=Menlo",
		padRight(label, 7),
		padRight(meter(u), 10),
		u,
		resetStr(resetsAt),
	)
}

// isAuthError returns true when the error message indicates the user needs to
// re-authenticate by running the Claude CLI. All such errors from accounts.go
// and api.go contain the word "CLI" or "stale".
func isAuthError(msg string) bool {
	return strings.Contains(msg, "CLI") || strings.Contains(msg, "stale") || strings.Contains(msg, "rejected")
}

func main() {
	// Sub-command: SwiftBar calls us back with this to toggle gauge visibility.
	if len(os.Args) >= 3 && os.Args[1] == "toggle-hidden" {
		if err := toggleHidden(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "toggle-hidden: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Sub-command: SwiftBar relaunches us in Terminal with this to self-update.
	if len(os.Args) >= 2 && os.Args[1] == updatecheck.SelfUpdateArg {
		updatecheck.RunSelfUpdate()
		return
	}

	accounts, err := discoverAccounts()
	if err != nil || len(accounts) == 0 {
		fmt.Println("◔ ? | color=orange")
		fmt.Println("---")
		fmt.Println("No Claude accounts found")
		fmt.Println("--Run 'claude' to log in | bash=/bin/bash param1=-l param2=-c param3=claude terminal=true")
		fmt.Println("Or pin accounts manually in ~/.config/claude-quota/accounts")
		return
	}

	type result struct {
		label string
		usage *Usage
		err   string
	}

	results := make([]result, 0, len(accounts))
	var oldestFetchedAt float64
	for _, acc := range accounts {
		token, tokenErr := getToken(acc.ConfigDir)
		var usage *Usage
		var errMsg string
		var fetchedAt float64
		if tokenErr != nil {
			errMsg = tokenErr.Error()
		} else {
			var fetchErr error
			usage, fetchedAt, fetchErr = fetchUsageCached(acc.ConfigDir, token)
			if fetchErr != nil && isAuthError(fetchErr.Error()) {
				// The keychain looked fine but the server rejected the token.
				// Renew once in the background and retry before giving up.
				renewToken(acc.ConfigDir)
				if token, tokenErr = getToken(acc.ConfigDir); tokenErr == nil {
					usage, fetchedAt, fetchErr = fetchUsageCached(acc.ConfigDir, token)
				} else {
					fetchErr = tokenErr
				}
			}
			if fetchErr != nil {
				errMsg = fetchErr.Error()
			}
		}
		if fetchedAt > 0 && (oldestFetchedAt == 0 || fetchedAt < oldestFetchedAt) {
			oldestFetchedAt = fetchedAt
		}
		results = append(results, result{acc.Label, usage, errMsg})
	}

	hidden, _ := loadHidden()

	var visible []BarResult
	for _, r := range results {
		if !hidden[r.label] {
			visible = append(visible, BarResult{r.label, r.usage, r.err != ""})
		}
	}

	imgB64, imgErr := menuBarImage(visible, len(results) > 1)
	if imgErr == nil {
		fmt.Printf("| image=%s\n", imgB64)
	} else {
		// Plain-text fallback if rendering breaks.
		var parts []string
		for _, r := range visible {
			first := "?"
			if runes := []rune(r.Name); len(runes) > 0 {
				first = string(runes[:1])
			}
			if r.Usage != nil && r.Usage.FiveHour != nil {
				parts = append(parts, fmt.Sprintf("%s %.0f", first, r.Usage.FiveHour.Utilization))
			} else {
				parts = append(parts, first+" ⚠")
			}
		}
		if len(parts) == 0 {
			fmt.Println("◔ …")
		} else {
			fmt.Println("◔ " + strings.Join(parts, " · "))
		}
	}

	// Use the real binary path so SwiftBar's callback re-invokes us correctly.
	script, err := os.Executable()
	if err != nil {
		script = os.Args[0]
	}

	toggleLine := func(name string) string {
		verb := "Hide from menu bar"
		if hidden[name] {
			verb = "Show in menu bar"
		}
		return fmt.Sprintf("--%s | bash=%s param1=toggle-hidden param2=%s terminal=false refresh=true",
			verb, script, name)
	}

	for _, r := range results {
		fmt.Println("---")
		if r.err != "" {
			fmt.Printf("%s: ⚠ %s\n", r.label, r.err)
			fmt.Println(toggleLine(r.label))
			if isAuthError(r.err) {
				// Background renewal already failed (dead refresh token), so offer
				// a one-click fix: open Terminal and run the claude CLI to log in.
				fmt.Println("--Re-authenticate (auto-renew failed): run 'claude' in Terminal | bash=/bin/bash param1=-l param2=-c param3=claude terminal=true")
			}
			continue
		}
		if hidden[r.label] {
			fmt.Printf("%s (hidden)\n", r.label)
		} else {
			fmt.Println(r.label)
		}
		fmt.Println(toggleLine(r.label))

		u := r.usage
		if u == nil {
			continue
		}
		if u.FiveHour != nil {
			fmt.Println(windowLine("5-hour", u.FiveHour))
		}
		if u.SevenDay != nil {
			fmt.Println(windowLine("week", u.SevenDay))
		}
		for _, sm := range u.ScopedModels() {
			fmt.Println(windowLineFields(sm.Label, sm.Percent, sm.ResetsAt))
		}
		if u.ExtraUsage != nil && u.ExtraUsage.IsEnabled && u.ExtraUsage.UsedCredits > 0 {
			fmt.Printf("extra   %.2f / %.0f %s | font=Menlo\n",
				u.ExtraUsage.UsedCredits/100,
				u.ExtraUsage.MonthlyLimit/100,
				strings.TrimSpace(u.ExtraUsage.Currency),
			)
		}
	}
	fmt.Println("---")
	refreshLabel := "⟳ Refresh now"
	if ts := badge.LastRefreshed(oldestFetchedAt); ts != "" {
		refreshLabel = fmt.Sprintf("⟳ Refresh now (last updated %s)", ts)
	}
	fmt.Println(refreshLabel + " | refresh=true")
	if line := updatecheck.MenuLine(script); line != "" {
		fmt.Println(line)
	}
}
