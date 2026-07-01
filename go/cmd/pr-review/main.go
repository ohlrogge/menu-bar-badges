package main

import (
	"errors"
	"fmt"
	"strings"
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

func main() {
	data, err := fetchGitHubCached()

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
			fmt.Println("Run 'gh auth login' in Terminal | bash=/bin/bash param1=-l param2=-c param3=\"gh auth login\" terminal=true")
		case errors.Is(err, errPinnedUser):
			fmt.Println("Pinned GitHub account is not available")
			fmt.Printf("Check ~/.config/pr-review/user or run 'gh auth login' for %s\n", configuredUser())
		default:
			fmt.Printf("⚠ %s\n", err)
		}
		fmt.Println("---")
		fmt.Println("Refresh now | refresh=true")
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
	fmt.Println("Refresh now | refresh=true")
}
