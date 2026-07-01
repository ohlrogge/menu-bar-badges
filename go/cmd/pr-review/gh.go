package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// errNoGh and errNoAuth let main.go render the right one-time-setup hint.
var (
	errNoGh       = errors.New("gh CLI not found")
	errNoAuth     = errors.New("gh not authenticated")
	errPinnedUser = errors.New("pinned account token unavailable")
)

// PR is one pull request from a GitHub search result.
type PR struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	IsDraft        bool   `json:"isDraft"`
	ReviewDecision string `json:"reviewDecision"`
	Repository     struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// Data holds both PR lists shown by the plugin.
type Data struct {
	ReviewRequested []PR `json:"reviewRequested"`
	Mine            []PR `json:"mine"`
}

// configuredUser returns the GitHub login from ~/.config/pr-review/user, or ""
// if the file doesn't exist. This is an opt-in override for users with multiple
// gh accounts who want to pin the plugin to a specific one.
func configuredUser() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".config", "pr-review", "user"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// tokenForUser returns the gh auth token for the given login via
// `gh auth token --user <login>`. Returns "" on any error.
func tokenForUser(gh, login string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, "auth", "token", "--user", login)
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ghPath resolves the gh binary. SwiftBar execs us without a login shell's
// PATH, so fall back to common install locations (Homebrew, Nix profile,
// Go/Cargo bins) after LookPath.
func ghPath() (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	candidates := []string{
		"/opt/homebrew/bin/gh",
		"/usr/local/bin/gh",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".nix-profile/bin/gh"),
			filepath.Join(home, ".local/bin/gh"),
		)
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errNoGh
}

// One GraphQL request returns both searches via aliases. @me resolves to the
// authenticated user. reviewDecision is requested here because gh's REST/search
// JSON does not expose it across repositories.
const graphqlQuery = `
{
  reviewRequested: search(query: "is:open is:pr review-requested:@me archived:false", type: ISSUE, first: 40) {
    nodes {
      ... on PullRequest {
        number title url isDraft reviewDecision
        repository { nameWithOwner }
        author { login }
      }
    }
  }
  mine: search(query: "is:open is:pr author:@me archived:false", type: ISSUE, first: 40) {
    nodes {
      ... on PullRequest {
        number title url isDraft reviewDecision
        repository { nameWithOwner }
        author { login }
      }
    }
  }
}`

type graphqlResp struct {
	Data struct {
		ReviewRequested struct {
			Nodes []PR `json:"nodes"`
		} `json:"reviewRequested"`
		Mine struct {
			Nodes []PR `json:"nodes"`
		} `json:"mine"`
	} `json:"data"`
}

// currentLogin returns the authenticated user's login, used to drop self-authored
// PRs from the review-requested list (you can't review your own PR).
func currentLogin(gh string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, "api", "user", "-q", ".login")
	cmd.Dir = os.TempDir()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fetchGitHub runs the GraphQL query and returns the parsed PR lists.
func fetchGitHub() (*Data, error) {
	gh, err := ghPath()
	if err != nil {
		return nil, err
	}

	query := graphqlQuery
	me := configuredUser()
	if me != "" {
		query = strings.ReplaceAll(query, "@me", me)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, gh, "api", "graphql", "-f", "query="+query)
	cmd.Dir = os.TempDir()
	if me != "" {
		// A pinned account must authenticate as itself. If its token can't be
		// resolved, refuse rather than silently querying as the active account —
		// that would return the wrong (private-repo) results with no indication.
		tok := tokenForUser(gh, me)
		if tok == "" {
			return nil, errPinnedUser
		}
		cmd.Env = append(os.Environ(), "GH_TOKEN="+tok)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		low := strings.ToLower(msg)
		if strings.Contains(low, "auth") || strings.Contains(low, "http 401") || strings.Contains(low, "gh auth login") {
			return nil, errNoAuth
		}
		if msg = strings.TrimSpace(msg); msg != "" {
			// Keep it short for the menu bar dropdown.
			if i := strings.IndexByte(msg, '\n'); i > 0 {
				msg = msg[:i]
			}
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("gh api failed: %w", err)
	}

	var resp graphqlResp
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("unexpected gh response")
	}

	if me == "" {
		me = currentLogin(gh)
	}
	data := &Data{
		ReviewRequested: filterOutAuthor(resp.Data.ReviewRequested.Nodes, me),
		Mine:            resp.Data.Mine.Nodes,
	}
	return data, nil
}

// filterOutAuthor drops PRs authored by login (and empty inline-fragment nodes).
func filterOutAuthor(prs []PR, login string) []PR {
	out := prs[:0]
	for _, pr := range prs {
		if pr.URL == "" {
			continue // non-PullRequest node
		}
		if login != "" && pr.Author.Login == login {
			continue
		}
		out = append(out, pr)
	}
	return out
}

// ---- caching (mirrors the claude-quota cache: short TTL, atomic writes,
// stale-data-beats-error on transient failures) ----

const cacheTTL = 30 * time.Second

type cacheEntry struct {
	FetchedAt float64 `json:"fetched_at"`
	Data      *Data   `json:"data"`
}

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".cache", "pr-review", "github.json")
}

// fetchGitHubCached serves cached data when fresh, and falls back to stale data
// rather than an error on transient failures. Auth/missing-gh errors are never
// masked by stale data, since they need user action.
func fetchGitHubCached() (*Data, error) {
	path := cacheFilePath()
	now := float64(time.Now().UnixNano()) / 1e9

	var cached cacheEntry
	if raw, err := os.ReadFile(path); err == nil {
		json.Unmarshal(raw, &cached) //nolint:errcheck // stale cache is acceptable
	}
	if cached.Data != nil && now-cached.FetchedAt < cacheTTL.Seconds() {
		return cached.Data, nil
	}

	data, err := fetchGitHub()
	if err != nil {
		if errors.Is(err, errNoGh) || errors.Is(err, errNoAuth) || errors.Is(err, errPinnedUser) {
			return nil, err
		}
		if cached.Data != nil {
			return cached.Data, nil // stale beats a transient error
		}
		return nil, err
	}

	saveCache(path, cacheEntry{FetchedAt: now, Data: data})
	return data, nil
}

func saveCache(path string, entry cacheEntry) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "cache-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	os.Rename(tmpName, path) //nolint:errcheck // best-effort on same filesystem
}
