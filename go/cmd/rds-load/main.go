package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
)

// consoleURL builds a Performance Insights deep link. This path is a
// best-effort format seen in AWS console traffic, not officially documented —
// if AWS changes it, the link just opens the RDS console instead of failing.
func consoleURL(region, resourceID string) string {
	return fmt.Sprintf("https://%s.console.aws.amazon.com/rds/home?region=%s#performance-insights-v20206:/resourceId/%s/dbtype/RDS",
		region, region, resourceID)
}

// loadLabel is a fixed-width bracketed prefix so the load value lines up
// down the (already load-sorted) list, e.g. "[ 7]", "[15]", "[ ?]", "[--]".
func loadLabel(db DBInstance) string {
	if !db.PIEnabled {
		return "[--]"
	}
	if db.Load == nil {
		return "[ ?]"
	}
	return fmt.Sprintf("[%2.0f]", *db.Load)
}

func dbLine(db DBInstance) string {
	label := fmt.Sprintf("%s %s (%s)", loadLabel(db), db.Id, db.Region)
	if !db.PIEnabled {
		return fmt.Sprintf("%s  PI disabled | font=Menlo", label)
	}
	if db.Load == nil {
		return fmt.Sprintf("%s  load unknown | font=Menlo href=%s", label, consoleURL(db.Region, db.ResourceID))
	}
	return fmt.Sprintf("%s | font=Menlo href=%s", label, consoleURL(db.Region, db.ResourceID))
}

// byLoadDesc sorts instances by load descending, with unknown-load instances
// (including PI-disabled ones) last.
func byLoadDesc(instances []DBInstance) []DBInstance {
	out := make([]DBInstance, len(instances))
	copy(out, instances)
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := out[i].Load, out[j].Load
		if li == nil || lj == nil {
			return li != nil // known loads sort before unknown ones
		}
		return *li > *lj
	})
	return out
}

// handleSetting is the callback invoked via a dropdown click's bash= action
// (see configLines). It persists the choice and drops the cache so the next
// refresh reflects it immediately, rather than serving a stale reading taken
// under the old setting.
func handleSetting(settingsKey string, allowed []int, arg string) {
	v, err := strconv.Atoi(arg)
	if err != nil || !isAllowed(v, allowed) {
		return
	}
	if err := saveSetting(settingsKey, v); err != nil {
		fmt.Fprintf(os.Stderr, "set %s: %v\n", settingsKey, err)
		return
	}
	os.Remove(cacheFilePath()) //nolint:errcheck // best-effort; a missing cache just means one slow refresh
}

// optionLine renders one clickable submenu entry, e.g. "--✓ 5m" for the
// active value or "--5m" for the others.
func optionLine(script, cmd string, value, current int) string {
	marker := ""
	if value == current {
		marker = "✓ "
	}
	return fmt.Sprintf("--%s%dm | bash=%s param1=%s param2=%d terminal=false refresh=true", marker, value, script, cmd, value)
}

// configLines renders the Averaging Window/Refresh submenus that let the user
// change either setting straight from the dropdown, without SwiftBar's
// separate plugin preferences UI. A change only takes visible effect once
// SwiftBar's refresh=true re-runs the plugin (and, since the click clears the
// cache, a fresh AWS fetch completes) — the leading info line makes that
// delay expected rather than looking like the click did nothing.
func configLines() []string {
	script, err := os.Executable()
	if err != nil {
		script = os.Args[0]
	}
	lines := []string{"Settings — changes apply after the next refresh | font=Menlo"}
	lines = append(lines, fmt.Sprintf("Averaging Window: %dm | font=Menlo", windowMinutes()))
	for _, v := range allowedWindowMinutes {
		lines = append(lines, optionLine(script, "set-window", v, windowMinutes()))
	}
	lines = append(lines, fmt.Sprintf("Refresh: %dm | font=Menlo", refreshMinutes()))
	for _, v := range allowedRefreshMinutes {
		lines = append(lines, optionLine(script, "set-refresh", v, refreshMinutes()))
	}
	return lines
}

func main() {
	if len(os.Args) >= 3 {
		switch os.Args[1] {
		case "set-window":
			handleSetting("window_minutes", allowedWindowMinutes, os.Args[2])
			return
		case "set-refresh":
			handleSetting("refresh_minutes", allowedRefreshMinutes, os.Args[2])
			return
		}
	}

	data, err := fetchAllCached()

	if err != nil {
		if img, imgErr := menuBarImage(0, false, true); imgErr == nil {
			fmt.Printf("| image=%s\n", img)
		} else {
			fmt.Println("DB ⚠")
		}
		fmt.Println("---")
		switch {
		case errors.Is(err, errNoAWSCli):
			fmt.Println("AWS CLI (aws) not found")
			fmt.Println("Install: brew install awscli | href=https://aws.amazon.com/cli/")
		case errors.Is(err, errNoAuth):
			fmt.Println("Not signed in to AWS")
			fmt.Println("Run 'aws sso login' in Terminal | bash=/bin/bash param1=-l param2=-c param3=\"aws sso login\" terminal=true")
		default:
			fmt.Printf("⚠ %s\n", err)
		}
		fmt.Println("---")
		fmt.Println("Refresh now | refresh=true")
		for _, line := range configLines() {
			fmt.Println(line)
		}
		return
	}

	var maxLoad float64
	hasData := false
	for _, db := range data.Instances {
		if db.Load == nil {
			continue
		}
		if !hasData || *db.Load > maxLoad {
			maxLoad = *db.Load
		}
		hasData = true
	}

	if img, imgErr := menuBarImage(maxLoad, hasData, false); imgErr == nil {
		fmt.Printf("| image=%s\n", img)
	} else if hasData {
		fmt.Printf("DB %.0f\n", maxLoad)
	} else {
		fmt.Println("DB ?")
	}

	fmt.Println("---")
	fmt.Printf("DB instances (%d)\n", len(data.Instances))
	if len(data.Instances) == 0 {
		fmt.Println("No RDS instances found")
	} else {
		for _, db := range byLoadDesc(data.Instances) {
			fmt.Println(dbLine(db))
		}
	}

	fmt.Println("---")
	fmt.Println("Refresh now | refresh=true")
	for _, line := range configLines() {
		fmt.Println(line)
	}
}
