package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Account holds a discovered Claude config directory with a display label.
type Account struct {
	Label     string
	ConfigDir string
}

func expandUser(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// keychainService derives the macOS Keychain service name for a config dir.
// Matches the naming convention used by Claude Code itself.
func keychainService(configDir string) string {
	if filepath.Base(configDir) == ".claude" {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(configDir))
	return fmt.Sprintf("Claude Code-credentials-%x", sum[:4])
}

func keychainEntryExists(service string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", service)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func defaultLabel(configDir string) string {
	name := filepath.Base(configDir)
	if name == ".claude" {
		return "Default"
	}
	label := strings.TrimPrefix(name, ".claude-")
	if label == "" {
		return "Default"
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

// discoverAccounts returns accounts from the pinned file if it exists,
// otherwise auto-discovers from ~/.claude* directories.
func discoverAccounts() ([]Account, error) {
	accountsFile := expandUser("~/.config/claude-quota/accounts")
	if _, err := os.Stat(accountsFile); err == nil {
		return loadAccountsFile(accountsFile)
	}
	return autoDiscoverAccounts()
}

func loadAccountsFile(path string) ([]Account, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var accounts []Account
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Split on first run of whitespace; remainder is the label.
		i := strings.IndexAny(line, " \t")
		var pathStr, label string
		if i < 0 {
			pathStr = line
		} else {
			pathStr = line[:i]
			label = strings.TrimSpace(line[i+1:])
		}
		dir := filepath.Clean(expandUser(pathStr))
		if label == "" {
			label = defaultLabel(dir)
		}
		accounts = append(accounts, Account{Label: label, ConfigDir: dir})
	}
	return accounts, scanner.Err()
}

func autoDiscoverAccounts() ([]Account, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(home, ".claude*"))
	if err != nil {
		return nil, err
	}
	var accounts []Account
	for _, path := range matches {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			continue
		}
		if keychainEntryExists(keychainService(path)) {
			accounts = append(accounts, Account{
				Label:     defaultLabel(path),
				ConfigDir: path,
			})
		}
	}
	if len(accounts) == 1 {
		accounts[0].Label = "Claude"
	}
	return accounts, nil
}

// ---- keychain token retrieval ----

type keychainCreds struct {
	ClaudeAiOauth struct {
		AccessToken string  `json:"accessToken"`
		ExpiresAt   float64 `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// renewBefore is how far ahead of expiry we proactively renew. It is at least
// the SwiftBar refresh interval (5m) so a token never expires between runs.
const renewBefore = 10 * time.Minute

// readKeychainToken reads the OAuth access token and its expiry from the macOS
// Keychain. Uses /usr/bin/security directly (no PATH lookup) to avoid binary
// substitution.
func readKeychainToken(configDir string) (token string, expiresAt float64, err error) {
	service := keychainService(configDir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security",
		"find-generic-password", "-s", service, "-w")
	out, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("no keychain entry — log in with that CLI once")
	}
	var creds keychainCreds
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &creds); err != nil {
		return "", 0, fmt.Errorf("unexpected credential format")
	}
	oa := creds.ClaudeAiOauth
	return oa.AccessToken, oa.ExpiresAt, nil
}

// renewToken runs Claude Code in non-interactive print mode, which refreshes
// the OAuth credentials in the Keychain using the stored refresh token. It is
// best-effort: a dead/missing refresh token just makes claude exit non-zero.
// Runs through a login shell so it picks up the PATH where claude lives,
// matching the Terminal fallback menu item.
func renewToken(configDir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-l", "-c", `claude -p "" 2>/dev/null`)
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+configDir)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run() // best-effort; caller re-reads the keychain to check success
}

// getToken returns a non-expired OAuth access token for configDir. When the
// token is missing or near expiry it renews silently in the background (via
// renewToken) and re-reads the keychain before giving up, so the Terminal
// fallback is only needed when the refresh token itself is dead.
func getToken(configDir string) (string, error) {
	token, expiresAt, err := readKeychainToken(configDir)
	if err != nil {
		// No keychain entry (or unreadable): renewal cannot help — a
		// never-logged-in account needs the interactive CLI.
		return "", err
	}

	stale := expiresAt > 0 && float64(time.Now().Add(renewBefore).UnixMilli()) > expiresAt
	if !stale {
		return token, nil
	}

	// Expired or about to expire but a keychain entry exists: try a silent renewal.
	renewToken(configDir)
	token, expiresAt, err = readKeychainToken(configDir)
	if err != nil {
		return "", err
	}
	if expiresAt > 0 && float64(time.Now().UnixMilli()) > expiresAt {
		return "", fmt.Errorf("token stale — run that CLI once to refresh")
	}
	return token, nil
}

// ---- hidden-accounts list ----

func hiddenFilePath() string {
	return expandUser("~/.config/claude-quota/hidden")
}

func loadHidden() (map[string]bool, error) {
	f, err := os.Open(hiddenFilePath())
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hidden := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			hidden[line] = true
		}
	}
	return hidden, scanner.Err()
}

// toggleHidden adds or removes label from the hidden list.
// Uses an exclusive flock to prevent races when SwiftBar runs concurrently.
func toggleHidden(label string) error {
	path := hiddenFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}

	hidden := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			hidden[line] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if hidden[label] {
		delete(hidden, label)
	} else {
		hidden[label] = true
	}

	names := make([]string, 0, len(hidden))
	for name := range hidden {
		names = append(names, name)
	}
	sort.Strings(names)

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, name := range names {
		fmt.Fprintln(w, name)
	}
	return w.Flush()
}
