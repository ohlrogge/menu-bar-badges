# menu-bar-badges

![menu-bar-badges in the macOS menu bar](docs/menu-bar.png)

*pr-review (left) and claude-quota (right) sitting in the menu bar.*

[SwiftBar](https://github.com/swiftbar/SwiftBar) plugins for the menu bar. The repo ships three:

- **claude-quota** — status gauges for your Claude Code quota, one rounded bar per account.
- **pr-review** — a badge counting GitHub PRs awaiting your review, with a dropdown of those PRs and your own open PRs.
- **rds-load** — a badge showing the highest Amazon RDS DB Load across all your instances, with a dropdown listing every instance.

SwiftBar is a free macOS app that runs scripts and binaries on a timer and displays their output in the menu bar. Each plugin is a compiled Go binary it runs every minute, so the badges (including the claude-quota reset countdown) stay current. Install any combination — the [installer](#quick-install) lets you choose.

> Forked from [grzegorz-raczek-unit8/claude-quota](https://github.com/grzegorz-raczek-unit8/claude-quota) and rewritten in Go.

## claude-quota — what it shows

- Each gauge displays the **5-hour-window utilisation** for one account.
- Fill colour shifts as the window fills up: **green** → **yellow** (≥60%) → **orange** (≥75%) → **red** (≥90%).
- When the 5-hour window is fully used, the gauge shows a **countdown to reset** (`4:28`).
- When the **weekly limit** is hit, the gauge turns **black** with a countdown to the weekly reset (`2D`).
- The dropdown lists full detail for every account: 5-hour and weekly windows, per-model windows where your plan reports them, extra-usage credits, and reset times.
- Refreshes every minute plus a manual **Refresh now** entry.
- If a token is stale, a **Re-authenticate** menu item opens Terminal and runs `claude` directly.

## pr-review — what it shows

- A single badge with the **count of open PRs where your review is requested** (across all of GitHub). The colour escalates with the count: **grey** (0) → **blue** (1–2) → **orange** (3–4) → **red** (5+).
- The dropdown has two sections:
  - **Review requested** — each PR awaiting your review, as a clickable link (with a `[draft]` marker where relevant).
  - **My open PRs** — your own open PRs with a status marker: ✓ approved, ✗ changes requested, ○ review needed, ✎ draft, · open.
- Refreshes every minute plus a manual **Refresh now** entry.

It uses the GitHub CLI (`gh`), so you need it installed and signed in — see [How pr-review works](#how-pr-review-works). If `gh` is missing or unauthenticated, the dropdown shows a one-time setup hint instead.

## rds-load — what it shows

- A single badge with the highest **DB Load** (Performance Insights `db.load.avg`, i.e. Average Active Sessions) across every RDS instance in every AWS-enabled region. This is a raw AAS value, not a percentage. The colour escalates: **green** (< 10) → **orange** (10–15) → **red** (≥ 15).
- The dropdown lists every DB instance, sorted by load descending, as `[<load>] <instance> (<region>)`. Clicking one opens its Performance Insights page in the AWS console. Instances without Performance Insights enabled show `[--]` and `PI disabled` instead of a load.
- Refreshes every minute plus a manual **Refresh now** entry.
- Two more dropdown entries, **Averaging Window** and **Refresh**, expand into a submenu of clickable values (`✓` marks the active one) — a note above them flags that a change applies after the next refresh. See [Averaging window and refresh interval](#averaging-window-and-refresh-interval).

It uses the AWS CLI (`aws`) with your default profile/session, so you need it installed and signed in — see [How rds-load works](#how-rds-load-works). If `aws` is missing or unauthenticated, the dropdown shows a one-time setup hint instead.

## Quick install

Requires macOS and [Homebrew](https://brew.sh). The installer also needs [Go](https://go.dev/dl/) to build, [SwiftBar](https://github.com/swiftbar/SwiftBar) to run the plugins, and — depending on which plugins you pick — the [GitHub CLI](https://cli.github.com) (`gh`) for pr-review or the [AWS CLI](https://aws.amazon.com/cli/) (`aws`) for rds-load; when run interactively it asks before installing any that are missing via Homebrew.

```sh
curl -fsSL https://raw.githubusercontent.com/ohlrogge/menu-bar-badges/main/install.sh | bash
```

The installer asks which plugins to install. To choose non-interactively (e.g. for `curl | bash`), set `PLUGINS`:

```sh
URL=https://raw.githubusercontent.com/ohlrogge/menu-bar-badges/main/install.sh
curl -fsSL "$URL" | PLUGINS=claude bash      # claude-quota only
curl -fsSL "$URL" | PLUGINS=gh bash          # pr-review only
curl -fsSL "$URL" | PLUGINS=rds bash         # rds-load only
curl -fsSL "$URL" | PLUGINS=claude,gh,rds bash   # all three
```

From a checkout you can instead pass `--claude`, `--gh`, `--rds`, or `--all`. When macOS shows a Keychain permission dialog on the first claude-quota refresh, click **Always Allow**.

## Install from a checkout

```sh
git clone https://github.com/ohlrogge/menu-bar-badges.git
cd menu-bar-badges
./install.sh
```

Both install paths set up [SwiftBar](https://github.com/swiftbar/SwiftBar) via Homebrew if it is not already installed, and add it to Login Items so the badges come back after a reboot.

## How claude-quota works

The plugin reads your Claude Code OAuth token from the macOS Keychain (**read-only** — it never refreshes or rewrites tokens, so it cannot log you out) and queries the same usage endpoint that Claude Code's `/usage` screen uses. No passwords, no scraping, no third-party services.

The binary calls `/usr/bin/security` directly (no `PATH` lookup) and writes cache files with `0600` permissions to `~/.cache/claude-quota/`.

> **Note:** the usage endpoint is internal to Claude Code and undocumented, so a future Claude Code update may require a small fix here.

## How pr-review works

The plugin shells out to the authenticated [GitHub CLI](https://cli.github.com) (`gh`) — there is no token handling in this code. It runs a single `gh api graphql` query for both PR lists and caches the result for 30s in `~/.cache/pr-review/`.

Sign in once with `gh auth login`. The query needs a token with the `repo` and `read:org` scopes — the defaults from `gh auth login` already cover this. If `gh` is missing or unauthenticated, the dropdown shows a setup hint instead of failing.

Because SwiftBar runs plugins without a login shell, the binary resolves `gh` by absolute path (Homebrew, Nix profile, `~/.local/bin`) rather than relying on `PATH`, so it keeps refreshing unattended.

### Multiple GitHub accounts

If you have more than one account logged into `gh`, the installer asks which one to use and writes your choice to `~/.config/pr-review/user`. The plugin then fetches that account's token via `gh auth token --user` and uses it for all API calls, so both the authentication and the PR search target the right account.

To change the pinned account after install, edit the file directly:

```sh
echo "your-work-username" > ~/.config/pr-review/user
```

To revert to the default active `gh` account, delete the file:

```sh
rm ~/.config/pr-review/user
```

## How rds-load works

The plugin shells out to the authenticated [AWS CLI](https://aws.amazon.com/cli/) (`aws`) using whatever profile/session it already resolves (`AWS_PROFILE` or default) — there is no separate account pinning like pr-review's. It lists every AWS-enabled region, then queries Performance Insights for `db.load.avg` on every PI-enabled DB instance, and caches the result in `~/.cache/rds-load/`.

Sign in once with `aws sso login` (or however your account authenticates). If `aws` is missing or unauthenticated, the dropdown shows a setup hint instead of failing.

### Averaging window and refresh interval

Two settings are configurable straight from the dropdown, under the **Averaging Window** and **Refresh** entries near the bottom — each expands into a submenu of values, and clicking one saves it (`✓` marks the active choice). A note above both submenus flags that the change only takes visible effect after the next refresh, since the click clears the cache and the following refresh needs to complete a fresh AWS fetch first:

- **Averaging Window** (default `5m`, one of `1`/`3`/`5`/`10`/`15`) — how far back `db.load.avg` is averaged. AWS returns one already-averaged value over this window, so a wider setting smooths out brief spikes (e.g. a jump to 12 that drops back within a minute) instead of flickering the badge.
- **Refresh** (default `1m`, one of `1`/`2`/`3`/`5`/`10`) — how often the plugin actually calls AWS. SwiftBar itself still invokes the binary every minute either way (that's fixed by the `.1m.cgo` filename); this setting is the plugin's own cache TTL, so a longer interval just serves cached data on the in-between runs instead of re-querying AWS every time.

The choice is saved to `~/.config/rds-load/settings` (`window_minutes=`, `refresh_minutes=`) and clears the cache so it takes effect on the very next refresh instead of waiting out the old TTL. Values outside the allowed list are ignored and the default is used instead.

`RDS_LOAD_WINDOW_MINUTES` / `RDS_LOAD_REFRESH_MINUTES` environment variables also work and take precedence over the settings file if you genuinely need to set them outside the dropdown — but note the installer does *not* declare these via SwiftBar's `<swiftbar.environment>` metadata, deliberately: that mechanism reinjects its declared value into every plugin invocation, which would silently fight the dropdown on every refresh.

### Large accounts: pinning regions

By default the plugin calls `describe-regions` and queries every enabled region, which can be slow for accounts with many regions enabled. To skip that scan, create `~/.config/rds-load/regions` with one region per line (`#` comments allowed):

```
us-east-1
eu-west-1
```

Delete the file to go back to querying all enabled regions.

## Accounts

By default the plugin auto-discovers accounts: every `~/.claude` / `~/.claude-*` config directory that has a Claude Code Keychain entry gets a gauge, labelled by the directory suffix (`~/.claude-work` → `W`). A single auto-discovered account shows no letter label — just the bar.

To pin or rename accounts, create `~/.config/claude-quota/accounts` with one `path [label]` per line:

```
~/.claude-work Work
~/.claude-priv Priv
```

To hide an account's menu bar gauge (its dropdown detail stays), use **Hide from menu bar** in the dropdown — or edit `~/.config/claude-quota/hidden` (one label per line).

Multiple accounts via `CLAUDE_CONFIG_DIR` look like this in your shell rc:

```sh
claude()      { CLAUDE_CONFIG_DIR="$HOME/.claude-work" command claude "$@"; }
claude-priv() { CLAUDE_CONFIG_DIR="$HOME/.claude-priv" command claude "$@"; }
```

## Uninstall

Run the uninstall script from a checkout — it asks about each item one by one:

```sh
./uninstall.sh
```

It covers plugin binaries, cached data, config files, the SwiftBar login item, and optionally the tools installed by `install.sh` (gh, aws, SwiftBar, Go).
