package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"claude-quota/internal/badge"
	"claude-quota/internal/updatecheck"
)

// sanitize strips characters that would break a SwiftBar menu line (| separates
// text from params; newlines split lines) and trims overly long titles.
func sanitize(s string) string {
	s = strings.ReplaceAll(s, "|", "¦")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	const maxLen = 60
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen-1]) + "…"
	}
	return s
}

// statusMarker summarizes one of the user's own PRs.
func statusMarker(pr PR) string {
	if pr.IsDraft {
		return "✎ draft"
	}
	switch pr.ReviewDecision {
	case "APPROVED":
		return "✓ approved"
	case "CHANGES_REQUESTED":
		return "✗ changes"
	case "REVIEW_REQUIRED":
		return "○ review needed"
	default:
		return "· open"
	}
}

func prLine(pr PR, prefix string) string {
	label := fmt.Sprintf("%s%s #%d %s", prefix, pr.Repository.NameWithOwner, pr.Number, sanitize(pr.Title))
	if pr.IsDraft && prefix == "" {
		label += " [draft]"
	}
	return fmt.Sprintf("%s | href=%s", label, pr.URL)
}

// refreshLine renders the "Refresh now" menu item, annotated with the last
// fetch time when known.
func refreshLine(fetchedAt float64) string {
	label := "⟳ Refresh now"
	if ts := badge.LastRefreshed(fetchedAt); ts != "" {
		label = fmt.Sprintf("⟳ Refresh now (last updated %s)", ts)
	}
	return label + " | refresh=true"
}

func main() {
	if len(os.Args) >= 2 && os.Args[1] == updatecheck.SelfUpdateArg {
		updatecheck.RunSelfUpdate()
		return
	}

	script, err := os.Executable()
	if err != nil {
		script = os.Args[0]
	}

	data, fetchedAt, err := fetchGitHubCached()

	if err != nil {
		// Menu bar: error badge.
		if img, imgErr := menuBarImage(0, true); imgErr == nil {
			fmt.Printf("| image=%s\n", img)
		} else {
			fmt.Println("PR ⚠")
		}
		fmt.Println("---")
		switch {
		case errors.Is(err, errNoGh):
			fmt.Println("GitHub CLI (gh) not found")
			fmt.Println("Install: brew install gh | href=https://cli.github.com")
		case errors.Is(err, errNoAuth):
			fmt.Println("Not signed in to GitHub")
			// bash=/bin/bash -c "gh auth login" doesn't survive SwiftBar's terminal=true
			// relaunch: it rejoins bash/paramN with plain spaces, so a quoted multi-word
			// param3 falls apart into separate argv/positional-params by the time bash -l
			// -c sees it, and Terminal is left stuck on an unterminated quote. Invoking the
			// gh binary directly with one arg per param sidesteps quoting entirely.
			if gh, ghErr := ghPath(); ghErr == nil {
				fmt.Printf("Run 'gh auth login' in Terminal | bash=%s param1=auth param2=login terminal=true\n", gh)
			} else {
				fmt.Println("Run 'gh auth login' in Terminal")
			}
		case errors.Is(err, errPinnedUser):
			fmt.Println("Pinned GitHub account is not available")
			fmt.Printf("Check ~/.config/pr-review/user or run 'gh auth login' for %s\n", configuredUser())
		default:
			fmt.Printf("⚠ %s\n", err)
		}
		fmt.Println("---")
		fmt.Println(refreshLine(fetchedAt))
		if line := updatecheck.MenuLine(script); line != "" {
			fmt.Println(line)
		}
		return
	}

	count := len(data.ReviewRequested)

	if img, imgErr := menuBarImage(count, false); imgErr == nil {
		fmt.Printf("| image=%s\n", img)
	} else {
		fmt.Printf("PR %d\n", count)
	}

	// Section 1: PRs awaiting my review.
	fmt.Println("---")
	fmt.Printf("Review requested (%d)\n", count)
	if count == 0 {
		fmt.Println("No reviews requested 🎉")
	} else {
		for _, pr := range data.ReviewRequested {
			fmt.Println(prLine(pr, ""))
		}
	}

	// Section 2: my own open PRs with status.
	fmt.Println("---")
	fmt.Printf("My open PRs (%d)\n", len(data.Mine))
	for _, pr := range data.Mine {
		fmt.Println(prLine(pr, statusMarker(pr)+"  "))
	}

	fmt.Println("---")
	fmt.Println(refreshLine(fetchedAt))
	if line := updatecheck.MenuLine(script); line != "" {
		fmt.Println(line)
	}
}
