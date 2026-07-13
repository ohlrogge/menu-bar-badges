package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// settingsFilePath holds the values picked from the dropdown's Window/Refresh
// submenus (settings.go), e.g. "window_minutes=5". Falls back silently to
// defaults if missing or unparsable — this file is a cache of user choices,
// not required configuration.
func settingsFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".config", "rds-load", "settings")
}

func loadSettingsFile() map[string]int {
	settings := map[string]int{}
	f, err := os.Open(settingsFilePath())
	if err != nil {
		return settings
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		settings[strings.TrimSpace(key)] = v
	}
	return settings
}

// saveSetting sets key=value in the settings file, preserving other keys.
// Uses an exclusive flock to prevent races when SwiftBar runs concurrently
// (mirrors claude-quota's toggleHidden).
func saveSetting(key string, value int) error {
	path := settingsFilePath()
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

	settings := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		settings[strings.TrimSpace(k)] = v
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	settings[key] = value

	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, k := range keys {
		fmt.Fprintf(w, "%s=%d\n", k, settings[k])
	}
	return w.Flush()
}
