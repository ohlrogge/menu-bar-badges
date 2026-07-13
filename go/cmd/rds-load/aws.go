package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Configurable two ways: clickable dropdown entries (main.go) that persist to
// the settings file (settings.go), or SwiftBar's plugin environment variables
// UI (installed as <swiftbar.environment> metadata, see install.sh) — env
// vars take precedence when set, for scripting/power users. Both are
// free-text/click inputs rather than an enforced dropdown at the OS level, so
// isAllowed validates and callers silently fall back to the default.
var (
	allowedWindowMinutes  = []int{1, 3, 5, 10, 15}
	allowedRefreshMinutes = []int{1, 2, 3, 5, 10}
)

func isAllowed(v int, allowed []int) bool {
	for _, a := range allowed {
		if a == v {
			return true
		}
	}
	return false
}

// windowMinutes is how far back (and the PI period-in-seconds) fetchLoad
// averages db.load.avg over. Wider windows smooth out brief spikes.
func windowMinutes() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("RDS_LOAD_WINDOW_MINUTES"))); err == nil && isAllowed(v, allowedWindowMinutes) {
		return v
	}
	if v, ok := loadSettingsFile()["window_minutes"]; ok && isAllowed(v, allowedWindowMinutes) {
		return v
	}
	return 5
}

// refreshMinutes controls how often the plugin actually calls AWS (the cache
// TTL), not how often SwiftBar invokes the binary — that stays fixed at 1m
// (rds-load.1m.cgo) so a shorter setting can take effect without reinstalling.
func refreshMinutes() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("RDS_LOAD_REFRESH_MINUTES"))); err == nil && isAllowed(v, allowedRefreshMinutes) {
		return v
	}
	if v, ok := loadSettingsFile()["refresh_minutes"]; ok && isAllowed(v, allowedRefreshMinutes) {
		return v
	}
	return 1
}

// errNoAWSCli and errNoAuth let main.go render the right one-time-setup hint.
var (
	errNoAWSCli = errors.New("aws CLI not found")
	errNoAuth   = errors.New("aws not authenticated")
)

// DBInstance is one RDS instance and its most recent Performance Insights load.
type DBInstance struct {
	Id         string   `json:"id"`
	Region     string   `json:"region"`
	ResourceID string   `json:"resourceId"`
	PIEnabled  bool     `json:"piEnabled"`
	Load       *float64 `json:"load,omitempty"`
}

// Data holds every RDS instance found across all queried regions.
type Data struct {
	Instances []DBInstance `json:"instances"`
}

// awsPath resolves the aws binary. SwiftBar execs us without a login shell's
// PATH, so fall back to common install locations after LookPath.
func awsPath() (string, error) {
	if p, err := exec.LookPath("aws"); err == nil {
		return p, nil
	}
	candidates := []string{
		"/opt/homebrew/bin/aws",
		"/usr/local/bin/aws",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local/bin/aws"))
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errNoAWSCli
}

// runAWS runs an aws CLI subcommand with a timeout and returns its stdout.
func runAWS(aws string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, aws, args...)
	cmd.Dir = os.TempDir()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("aws command failed: %w", err)
		}
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return stdout.Bytes(), nil
}

// checkAuth confirms the default AWS CLI profile/session is usable.
func checkAuth(aws string) error {
	if _, err := runAWS(aws, 10*time.Second, "sts", "get-caller-identity", "--output", "json"); err != nil {
		return errNoAuth
	}
	return nil
}

// regionsOverridePath is an optional escape hatch for accounts with many
// enabled regions, where a full describe-regions scan is slow.
func regionsOverridePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "rds-load", "regions")
}

func loadRegionsFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var regions []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		regions = append(regions, line)
	}
	return regions, scanner.Err()
}

// enabledRegions returns the AWS-enabled regions to query, from the override
// file if present, otherwise from describe-regions. --region eu-west-1
// is hardcoded for that one call only: describe-regions needs some regional
// endpoint to call, but its result is account-wide regardless, so this must
// work even when the user has no default region configured.
func enabledRegions(aws string) ([]string, error) {
	if path := regionsOverridePath(); path != "" {
		if _, err := os.Stat(path); err == nil {
			return loadRegionsFile(path)
		}
	}
	out, err := runAWS(aws, 15*time.Second, "ec2", "describe-regions",
		"--region", "eu-west-1", "--output", "json", "--query", "Regions[].RegionName")
	if err != nil {
		return nil, fmt.Errorf("describe-regions: %w", err)
	}
	var regions []string
	if err := json.Unmarshal(out, &regions); err != nil {
		return nil, fmt.Errorf("unexpected describe-regions response")
	}
	return regions, nil
}

func listDBInstances(aws, region string) ([]DBInstance, error) {
	out, err := runAWS(aws, 15*time.Second, "rds", "describe-db-instances",
		"--region", region, "--output", "json",
		"--query", "DBInstances[].{Id:DBInstanceIdentifier,ResourceId:DbiResourceId,PI:PerformanceInsightsEnabled}")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Id         string `json:"Id"`
		ResourceId string `json:"ResourceId"`
		PI         bool   `json:"PI"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("unexpected describe-db-instances response")
	}
	instances := make([]DBInstance, 0, len(raw))
	for _, r := range raw {
		instances = append(instances, DBInstance{
			Id:         r.Id,
			Region:     region,
			ResourceID: r.ResourceId,
			PIEnabled:  r.PI,
		})
	}
	return instances, nil
}

// fetchLoad returns the db.load.avg (Average Active Sessions) for resourceID,
// averaged by AWS over the last windowMinutes() (period-in-seconds matches the
// window so a single request returns one already-averaged datapoint), or an
// error if no data point is available. A wider window smooths out brief
// spikes that would otherwise make the badge flicker. Callers treat an error
// as "load unknown" for that instance rather than failing the run.
func fetchLoad(aws, region, resourceID string) (*float64, error) {
	now := time.Now().UTC()
	window := time.Duration(windowMinutes()) * time.Minute
	out, err := runAWS(aws, 15*time.Second, "pi", "get-resource-metrics",
		"--region", region,
		"--service-type", "RDS",
		"--identifier", resourceID,
		"--metric-queries", "Metric=db.load.avg",
		"--start-time", now.Add(-window).Format(time.RFC3339),
		"--end-time", now.Format(time.RFC3339),
		"--period-in-seconds", strconv.Itoa(int(window.Seconds())),
		"--output", "json")
	if err != nil {
		return nil, err
	}
	var resp struct {
		MetricList []struct {
			DataPoints []struct {
				Value *float64 `json:"Value"`
			} `json:"DataPoints"`
		} `json:"MetricList"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("unexpected get-resource-metrics response")
	}
	var last *float64
	for _, m := range resp.MetricList {
		for _, dp := range m.DataPoints {
			if dp.Value != nil {
				v := *dp.Value
				last = &v
			}
		}
	}
	if last == nil {
		return nil, fmt.Errorf("no data points")
	}
	return last, nil
}

// maxConcurrentAWSCalls bounds the worker pool shared by both the per-region
// and per-DB fan-out below.
const maxConcurrentAWSCalls = 8

// fetchAll resolves the aws CLI, checks auth, lists DB instances across every
// enabled region, and fetches Performance Insights load for each PI-enabled
// instance, all bounded by a shared semaphore.
func fetchAll() (*Data, error) {
	aws, err := awsPath()
	if err != nil {
		return nil, err
	}
	if err := checkAuth(aws); err != nil {
		return nil, err
	}
	regions, err := enabledRegions(aws)
	if err != nil {
		return nil, err
	}

	sem := make(chan struct{}, maxConcurrentAWSCalls)
	var mu sync.Mutex
	var instances []DBInstance
	var wg sync.WaitGroup
	for _, region := range regions {
		region := region
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			list, err := listDBInstances(aws, region)
			if err != nil {
				return // best-effort across regions; skip on failure
			}
			mu.Lock()
			instances = append(instances, list...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	var wg2 sync.WaitGroup
	for i := range instances {
		if !instances[i].PIEnabled {
			continue
		}
		i := i
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			load, err := fetchLoad(aws, instances[i].Region, instances[i].ResourceID)
			if err == nil {
				instances[i].Load = load
			}
		}()
	}
	wg2.Wait()

	sort.Slice(instances, func(a, b int) bool {
		if instances[a].Region != instances[b].Region {
			return instances[a].Region < instances[b].Region
		}
		return instances[a].Id < instances[b].Id
	})

	return &Data{Instances: instances}, nil
}

// ---- caching (mirrors pr-review's cache: short TTL, atomic writes,
// stale-data-beats-error on transient failures) ----

// cacheTTL is refreshMinutes(), converted. SwiftBar itself still invokes this
// binary every 1 minute regardless (see rds-load.1m.cgo); this TTL is what
// actually governs how often AWS gets called, so a user-configured refresh
// interval takes effect without reinstalling with a different filename.
func cacheTTL() time.Duration {
	return time.Duration(refreshMinutes()) * time.Minute
}

type cacheEntry struct {
	FetchedAt float64 `json:"fetched_at"`
	Data      *Data   `json:"data"`
}

func cacheFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".cache", "rds-load", "data.json")
}

// fetchAllCached serves cached data when fresh, and falls back to stale data
// rather than an error on transient failures. Auth/missing-CLI errors are
// never masked by stale data, since they need user action. The returned
// float64 is the Unix-seconds time the data was actually fetched.
func fetchAllCached() (*Data, float64, error) {
	path := cacheFilePath()
	now := float64(time.Now().UnixNano()) / 1e9

	var cached cacheEntry
	if raw, err := os.ReadFile(path); err == nil {
		json.Unmarshal(raw, &cached) //nolint:errcheck // stale cache is acceptable
	}
	if cached.Data != nil && now-cached.FetchedAt < cacheTTL().Seconds() {
		return cached.Data, cached.FetchedAt, nil
	}

	data, err := fetchAll()
	if err != nil {
		if errors.Is(err, errNoAWSCli) || errors.Is(err, errNoAuth) {
			return nil, 0, err
		}
		if cached.Data != nil {
			return cached.Data, cached.FetchedAt, nil // stale beats a transient error
		}
		return nil, 0, err
	}

	saveCache(path, cacheEntry{FetchedAt: now, Data: data})
	return data, now, nil
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
