// Package updatecheck checks once per day whether origin/main has commits
// beyond the one this binary was built from, and offers a menu line to pull
// and rebuild.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BuiltSHA is set via -ldflags at build time (install.sh). Empty for
// `go run`/dev builds and for a build where `git rev-parse` failed, both of
// which disable the check entirely.
var BuiltSHA string

const (
	remoteURL     = "https://github.com/ohlrogge/menu-bar-badges.git"
	checkInterval = 24 * time.Hour
	installerURL  = "https://raw.githubusercontent.com/ohlrogge/menu-bar-badges/main/install.sh"

	// SelfUpdateArg is the subcommand each plugin's main() dispatches to
	// RunSelfUpdate, matching the existing toggle-hidden/set-window convention
	// (see claude-quota/main.go, rds-load/main.go). Keeping the whole
	// multi-step update command inside Go rather than a shell string handed
	// to SwiftBar's bash=/param3= sidesteps the param-rejoining bugs already
	// hit twice in this repo's history (commits 640abc4, f5f7ce3, 0973f2f):
	// SwiftBar's terminal=true relaunch only has to pass one plain word.
	SelfUpdateArg = "self-update"
)

type state struct {
	LastChecked float64 `json:"last_checked,omitempty"`
	LatestSHA   string  `json:"latest_sha,omitempty"`
}

func expandUser(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func cacheFilePath() string {
	return filepath.Join(expandUser("~/.cache/menu-bar-badges"), "update-check.json")
}

func loadState() state {
	var st state
	if data, err := os.ReadFile(cacheFilePath()); err == nil {
		json.Unmarshal(data, &st) //nolint:errcheck // stale/missing cache is acceptable
	}
	return st
}

func saveState(st state) {
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	path := cacheFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "update-check-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
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

// gitCandidates are checked in order when looking for the git binary. Avoids
// a PATH lookup: SwiftBar execs plugins directly, not via a login shell, so
// PATH may not include Homebrew's bin dirs (same rationale as findClaude in
// claude-quota/accounts.go and the gh candidates in pr-review/gh.go).
var gitCandidates = []string{
	"/usr/bin/git",
	"/opt/homebrew/bin/git",
	"/usr/local/bin/git",
}

func findGit() string {
	for _, p := range gitCandidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func fetchRemoteSHA() (string, error) {
	git := findGit()
	if git == "" {
		return "", fmt.Errorf("git not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, git, "ls-remote", remoteURL, "refs/heads/main").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("unexpected ls-remote output")
	}
	return fields[0], nil
}

// Available reports whether origin/main has moved past the commit this
// binary was built from. Checks the network at most once per 24h (cached);
// on a network failure it falls back to the last cached comparison rather
// than surfacing an error, matching fetchUsageCached's stale-beats-error
// philosophy in claude-quota/api.go.
func Available() bool {
	if BuiltSHA == "" || BuiltSHA == "unknown" {
		return false
	}
	st := loadState()
	now := float64(time.Now().UnixNano()) / 1e9
	if st.LastChecked > 0 && now-st.LastChecked < checkInterval.Seconds() {
		return st.LatestSHA != "" && st.LatestSHA != BuiltSHA
	}
	latest, err := fetchRemoteSHA()
	if err != nil {
		return st.LatestSHA != "" && st.LatestSHA != BuiltSHA
	}
	saveState(state{LastChecked: now, LatestSHA: latest})
	return latest != BuiltSHA
}

// MenuLine returns the SwiftBar line offering the update, or "" when none is
// available. script is the currently running binary's path (os.Executable()),
// used so the click re-invokes this same binary with SelfUpdateArg.
func MenuLine(script string) string {
	if !Available() {
		return ""
	}
	return fmt.Sprintf("⬆ Update available | bash=%s param1=%s terminal=true refresh=true", script, SelfUpdateArg)
}

// RunSelfUpdate downloads and runs install.sh. Called from main() when
// os.Args[1] == SelfUpdateArg (dispatched via a Terminal window opened by
// SwiftBar's terminal=true), so its output is visible to the user live.
func RunSelfUpdate() {
	script := fmt.Sprintf("curl -fsSL %s -o /tmp/menu-bar-badges-install.sh && bash /tmp/menu-bar-badges-install.sh", installerURL)
	cmd := exec.Command("/bin/bash", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	_ = cmd.Run()
}
