# claude-quota

Battery-style menu bar gauges for your Claude Code quota — one pill per
account, like this:

![menu bar screenshot](docs/menubar.png)

*(drawn for dark menu bars — white outlines)*

- Each pill shows the **5-hour-window utilization** for one account, colored
  green / orange (≥70%) / red (≥90%).
- When a window is fully used, the pill shows a **countdown until reset**
  (`4:28`) instead of the percentage.
- The dropdown shows full detail per account: 5-hour and weekly windows,
  per-model windows where your plan reports them, extra-usage credits, and
  reset times.
- Refreshes every 5 minutes (SwiftBar filename convention) plus a manual
  "Refresh now" entry.

## How it works

The plugin reads your Claude Code OAuth token from the macOS Keychain
(**read-only** — it never refreshes or rewrites tokens, so it can't log you
out) and queries the same usage endpoint that Claude Code's `/usage` screen
uses. No passwords, no scraping, no third-party services.

> **Note:** that endpoint is internal to Claude Code and undocumented, so a
> future Claude Code change may require a small fix here.

## Install

Requires macOS and [Homebrew](https://brew.sh) (to install
[SwiftBar](https://github.com/swiftbar/SwiftBar) if you don't have it).

```sh
git clone https://github.com/grzegorz-raczek-unit8/claude-quota.git
cd claude-quota
./install.sh
```

When macOS shows a Keychain permission dialog on the first refresh, click
**Always Allow**. If an account shows ⚠, its token went stale from disuse —
run that `claude` CLI once and the widget recovers on the next cycle.

## Accounts

By default the plugin auto-discovers accounts: every `~/.claude` /
`~/.claude-*` config directory that has a Claude Code Keychain entry gets a
pill, labeled by the directory suffix (`~/.claude-work` → `W`).

To pin or rename accounts (e.g. you use multiple `CLAUDE_CONFIG_DIR`s), create
`~/.config/claude-quota/accounts` with one `path [label]` per line:

```
~/.claude-work Work
~/.claude-priv Priv
```

Multiple accounts via `CLAUDE_CONFIG_DIR` look like this in your shell rc:

```sh
claude()      { CLAUDE_CONFIG_DIR="$HOME/.claude-work" command claude "$@"; }
claude-priv() { CLAUDE_CONFIG_DIR="$HOME/.claude-priv" command claude "$@"; }
```

## Uninstall

Delete `claude-quota.5m.py` from your SwiftBar plugin folder
(`~/.swiftbar` by default).
