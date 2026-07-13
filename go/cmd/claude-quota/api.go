package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	usageURL = "https://api.anthropic.com/api/oauth/usage"
	cacheTTL = 30 * time.Second
)

// Window represents a quota window (e.g. five-hour or seven-day).
type Window struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// ExtraUsage holds pay-as-you-go credit data.
type ExtraUsage struct {
	IsEnabled    bool    `json:"is_enabled"`
	UsedCredits  float64 `json:"used_credits"`
	MonthlyLimit float64 `json:"monthly_limit"`
	Currency     string  `json:"currency"`
}

// Usage holds the full API response.
type Usage struct {
	FiveHour       *Window     `json:"five_hour"`
	SevenDay       *Window     `json:"seven_day"`
	SevenDayOpus   *Window     `json:"seven_day_opus"`
	SevenDaySonnet *Window     `json:"seven_day_sonnet"`
	ExtraUsage     *ExtraUsage `json:"extra_usage"`
}

type cacheEntry struct {
	FetchedAt    float64 `json:"fetched_at,omitempty"`
	BackoffUntil float64 `json:"backoff_until,omitempty"`
	Usage        *Usage  `json:"usage,omitempty"`
}

// fetchUsage calls the Anthropic usage endpoint.
// Returns (usage, error, retryAfterSeconds); retryAfterSeconds is non-zero only on 429.
func fetchUsage(token string) (*Usage, error, int) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return nil, err, 0
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("offline?"), 0
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("token rejected — run that CLI once to refresh"), 0
	case http.StatusTooManyRequests:
		retryAfter := 300
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			fmt.Sscanf(ra, "%d", &retryAfter) //nolint:errcheck
		}
		return nil, fmt.Errorf("rate-limited"), retryAfter
	default:
		return nil, fmt.Errorf("API error %d", resp.StatusCode), 0
	}

	// Cap response to 512 KiB to guard against runaway responses.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("read error: %w", err), 0
	}
	var usage Usage
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, fmt.Errorf("unexpected API response"), 0
	}
	return &usage, nil, 0
}

func cacheFilePath(configDir string) string {
	sum := sha256.Sum256([]byte(configDir))
	key := fmt.Sprintf("%x", sum[:4]) // 8 hex chars, matching Python
	return filepath.Join(expandUser("~/.cache/claude-quota"), key+".json")
}

// fetchUsageCached serves cached data when fresh, backs off on 429, and falls
// back to stale data rather than showing an error on transient failures. The
// returned float64 is the Unix-seconds time the data was actually fetched.
func fetchUsageCached(configDir, token string) (*Usage, float64, error) {
	path := cacheFilePath(configDir)
	now := float64(time.Now().UnixNano()) / 1e9

	var cached cacheEntry
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &cached) //nolint:errcheck // stale cache is acceptable
	}

	if cached.Usage != nil && now-cached.FetchedAt < cacheTTL.Seconds() {
		return cached.Usage, cached.FetchedAt, nil
	}
	if cached.BackoffUntil > 0 && now < cached.BackoffUntil {
		if cached.Usage != nil {
			return cached.Usage, cached.FetchedAt, nil
		}
		return nil, 0, fmt.Errorf("rate-limited — retrying later")
	}

	usage, fetchErr, retryAfter := fetchUsage(token)

	saveCache := func(entry cacheEntry) {
		data, err := json.Marshal(entry)
		if err != nil {
			return
		}
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return
		}
		// Write via temp file + rename for atomicity.
		// os.CreateTemp creates with 0600, fixing the world-readable finding.
		tmp, err := os.CreateTemp(dir, "cache-*.tmp")
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

	if usage != nil {
		saveCache(cacheEntry{FetchedAt: now, Usage: usage})
		return usage, now, nil
	}
	if retryAfter > 0 {
		cached.BackoffUntil = now + float64(retryAfter)
		saveCache(cached)
	}
	if cached.Usage != nil {
		// Stale data beats an error display on transient failures.
		return cached.Usage, cached.FetchedAt, nil
	}
	return nil, 0, fetchErr
}
