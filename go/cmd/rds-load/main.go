package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"

	"claude-quota/internal/badge"
)

// consoleURL builds a Database Insights (CloudWatch) deep link for the given
// instance. AWS retired the Performance Insights console experience on July
// 31, 2026 in favor of Database Insights in CloudWatch. This path is a
// best-effort format seen in AWS console traffic, not officially documented —
// if AWS changes it, the link just opens the CloudWatch console instead of
// failing.
func consoleURL(region, id, resourceID string) string {
	return fmt.Sprintf("https://%s.console.aws.amazon.com/cloudwatch/home?region=%s#database-insights:instances"+
		"?selectedInstanceName=%%22%s%%22"+
		"&selectedInstanceResourceId=%%22%s%%22"+
		"&regionFilterQuery=%%5B%%22%s%%22%%5D"+
		"&filterQuery=%%7B%%22tokens%%22%%3A%%5B%%5D%%2C%%22operation%%22%%3A%%22or%%22%%7D"+
		"&timeRange=%%7B%%22type%%22%%3A%%22relative%%22%%2C%%22value%%22%%3A%%22PT30M%%22%%7D"+
		"&crossAccountMode=true&showInAlarm=true&showInOk=false&instanceSelectedTab=%%22dbLoadAnalysis%%22",
		region, region, id, resourceID, region)
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
		return fmt.Sprintf("%s  load unknown | font=Menlo href=%s", label, consoleURL(db.Region, db.Id, db.ResourceID))
	}
	return fmt.Sprintf("%s | font=Menlo href=%s", label, consoleURL(db.Region, db.Id, db.ResourceID))
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
	lines := []string{"---", "Settings (Changes apply after the next refresh)"}
	lines = append(lines, fmt.Sprintf("Averaging Window [%dm]", windowMinutes()))
	for _, v := range allowedWindowMinutes {
		lines = append(lines, optionLine(script, "set-window", v, windowMinutes()))
	}
	lines = append(lines, fmt.Sprintf("Refresh Period [%dm]", refreshMinutes()))
	for _, v := range allowedRefreshMinutes {
		lines = append(lines, optionLine(script, "set-refresh", v, refreshMinutes()))
	}
	return lines
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

	data, fetchedAt, err := fetchAllCached()

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
			// bash=/bin/bash -c "aws sso login" doesn't survive SwiftBar's terminal=true
			// relaunch: it rejoins bash/paramN with plain spaces, so a quoted multi-word
			// param3 falls apart into separate argv/positional-params by the time bash -l
			// -c sees it, and Terminal is left stuck on an unterminated quote. Invoking the
			// aws binary directly with one arg per param sidesteps quoting entirely.
			if aws, awsErr := awsPath(); awsErr == nil {
				fmt.Printf("Run 'aws sso login' in Terminal | bash=%s param1=sso param2=login terminal=true\n", aws)
			} else {
				fmt.Println("Run 'aws sso login' in Terminal")
			}
		default:
			fmt.Printf("⚠ %s\n", err)
		}
		for _, line := range configLines() {
			fmt.Println(line)
		}
		fmt.Println("---")
		fmt.Println(refreshLine(fetchedAt))
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

	for _, line := range configLines() {
		fmt.Println(line)
	}
	fmt.Println("---")
	fmt.Println(refreshLine(fetchedAt))
}
