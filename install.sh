#!/usr/bin/env bash
# Installs the SwiftBar plugins from this repo (compiled Go binaries).
#
# Plugins:
#   claude-quota  — Claude Code usage gauges in the menu bar
#   pr-review     — GitHub PRs awaiting your review (needs the gh CLI)
#   rds-load      — Amazon RDS DB Load per instance (needs the aws CLI)
#
# Choose what to install interactively, or non-interactively with either
# flags (--claude / --gh / --rds / --all) or the PLUGINS env var (e.g. PLUGINS=claude,gh).
set -euo pipefail

if [ "$(uname)" != "Darwin" ]; then
    echo "macOS only." >&2
    exit 1
fi

# ---- resolve plugin dir & detect existing installs ------------------------
# Resolved early (moved up from its original spot further down) so the plugin
# picker below can default to whatever's already installed.
PLUGIN_DIR=$(defaults read com.ameba.SwiftBar PluginDirectory 2>/dev/null || true)
if [ -z "$PLUGIN_DIR" ]; then
    PLUGIN_DIR="$HOME/.swiftbar"
    defaults write com.ameba.SwiftBar PluginDirectory -string "$PLUGIN_DIR"
fi
mkdir -p "$PLUGIN_DIR"

detect_installed() {
    for f in "$PLUGIN_DIR/$1".*; do
        [ -e "$f" ] && return 0
    done
    return 1
}
ALREADY_CLAUDE=false; detect_installed claude-quota && ALREADY_CLAUDE=true
ALREADY_GH=false;     detect_installed pr-review    && ALREADY_GH=true
ALREADY_RDS=false;    detect_installed rds-load     && ALREADY_RDS=true
ANY_INSTALLED=false
if [ "$ALREADY_CLAUDE" = true ] || [ "$ALREADY_GH" = true ] || [ "$ALREADY_RDS" = true ]; then
    ANY_INSTALLED=true
fi

# ---- choose plugins -------------------------------------------------------
INSTALL_CLAUDE=false
INSTALL_GH=false
INSTALL_RDS=false

for arg in "$@"; do
    case "$arg" in
        --claude) INSTALL_CLAUDE=true ;;
        --gh|--pr|--pr-review) INSTALL_GH=true ;;
        --rds|--aws) INSTALL_RDS=true ;;
        --all|--both) INSTALL_CLAUDE=true; INSTALL_GH=true; INSTALL_RDS=true ;;
    esac
done

if [ -n "${PLUGINS:-}" ]; then
    case ",$PLUGINS," in *,claude,*|*,claude-quota,*) INSTALL_CLAUDE=true ;; esac
    case ",$PLUGINS," in *,gh,*|*,pr,*|*,pr-review,*)  INSTALL_GH=true ;; esac
    case ",$PLUGINS," in *,rds,*|*,aws,*|*,rds-load,*) INSTALL_RDS=true ;; esac
fi

if [ "$INSTALL_CLAUDE" = false ] && [ "$INSTALL_GH" = false ] && [ "$INSTALL_RDS" = false ]; then
    # Default list reflects what's already installed, so re-running the
    # installer (e.g. via the "Update available" menu item) reinstalls only
    # what's there instead of silently adding plugins the user never chose.
    # A fresh machine with nothing installed still defaults to "all".
    default_list=""
    [ "$ALREADY_CLAUDE" = true ] && default_list="${default_list}1,"
    [ "$ALREADY_GH" = true ] && default_list="${default_list}2,"
    [ "$ALREADY_RDS" = true ] && default_list="${default_list}3,"
    default_list="${default_list%,}"
    if [ -z "$default_list" ]; then
        default_list="1,2,3"
    fi

    apply_choice() {
        case ",$1," in
            *,all,*) INSTALL_CLAUDE=true; INSTALL_GH=true; INSTALL_RDS=true; return ;;
        esac
        case ",$1," in *,1,*) INSTALL_CLAUDE=true ;; esac
        case ",$1," in *,2,*) INSTALL_GH=true ;; esac
        case ",$1," in *,3,*) INSTALL_RDS=true ;; esac
    }

    if [ -e /dev/tty ]; then
        echo "Which plugins do you want to install/update?"
        echo "  1) claude-quota  — Claude Code usage gauges$( [ "$ALREADY_CLAUDE" = true ] && echo ' (installed)')"
        echo "  2) pr-review     — GitHub PRs awaiting your review$( [ "$ALREADY_GH" = true ] && echo ' (installed)')"
        echo "  3) rds-load      — Amazon RDS DB Load$( [ "$ALREADY_RDS" = true ] && echo ' (installed)')"
        printf "Choice (comma-separated, or 'all') [%s]: " "$default_list"
        read -r choice </dev/tty
        choice="${choice:-$default_list}"
        apply_choice "$choice"
    else
        if [ "$ANY_INSTALLED" = true ]; then
            echo "No selection given; reinstalling currently installed plugins ($default_list) (use PLUGINS=claude, PLUGINS=gh, or PLUGINS=rds to change)."
        else
            echo "No selection given; installing all (use PLUGINS=claude, PLUGINS=gh, or PLUGINS=rds to choose)."
        fi
        apply_choice "$default_list"
    fi
fi

# ---- helpers --------------------------------------------------------------
# confirm: ask a yes/no question, default yes. When non-interactive (e.g.
# `curl … | bash`, where there is no one to ask) it assumes yes so the
# one-liner stays turnkey.
confirm() {
    local reply
    if [ -e /dev/tty ]; then
        printf "%s [Y/n] " "$1"
        read -r reply </dev/tty
        case "$reply" in [Nn]*) return 1 ;; esac
    fi
    return 0
}

# offer_brew_install <display name> <brew args…>: ask before installing a
# missing dependency via Homebrew. Returns non-zero if it can't or won't.
offer_brew_install() {
    local name="$1"; shift
    local cmd="brew $*"
    if ! command -v brew >/dev/null; then
        echo "$name is required, but Homebrew isn't installed (https://brew.sh)." >&2
        echo "Install Homebrew, then run:  $cmd" >&2
        return 1
    fi
    if confirm "$name is not installed. Install it now with '$cmd'?"; then
        brew "$@"
    else
        echo "Skipped. Install $name yourself with:  $cmd" >&2
        return 1
    fi
}

# ---- prerequisites --------------------------------------------------------
if ! command -v go >/dev/null 2>&1; then
    offer_brew_install "Go" install go || exit 1
fi

if [ ! -d "/Applications/SwiftBar.app" ]; then
    offer_brew_install "SwiftBar" install --cask swiftbar || exit 1
fi

if [ "$INSTALL_GH" = true ]; then
    if ! command -v gh >/dev/null 2>&1; then
        offer_brew_install "GitHub CLI (gh)" install gh || exit 1
    fi
    if ! gh auth status >/dev/null 2>&1; then
        echo "Note: you are not signed in to GitHub. Run 'gh auth login' once so the"
        echo "pr-review plugin can fetch your PRs."
    elif [ -e /dev/tty ]; then
        # If multiple accounts are logged in, ask which one to pin.
        gh_users=$(gh auth status 2>&1 | grep -oE 'account [a-zA-Z0-9._-]+' | awk '{print $2}' | sort -u)
        user_count=$(printf '%s\n' "$gh_users" | grep -c . 2>/dev/null || echo 0)
        config_file="$HOME/.config/pr-review/user"
        if [ "$user_count" -gt 1 ] && [ ! -f "$config_file" ]; then
            echo ""
            echo "Multiple GitHub accounts detected. Which one should pr-review use?"
            i=1
            while IFS= read -r u; do
                echo "  $i) $u"
                i=$((i + 1))
            done <<< "$gh_users"
            printf "Choice [1]: "
            read -r choice </dev/tty
            choice="${choice:-1}"
            # Guard against non-numeric / out-of-range input: sed with a bad
            # address would fail and, under `set -e`, abort the whole install.
            selected=""
            if printf '%s' "$choice" | grep -qE '^[0-9]+$' && [ "$choice" -ge 1 ] && [ "$choice" -lt "$i" ]; then
                selected=$(printf '%s\n' "$gh_users" | sed -n "${choice}p")
            fi
            if [ -z "$selected" ]; then
                echo "No valid choice made; skipping account pinning (edit ~/.config/pr-review/user later)."
            fi
            if [ -n "$selected" ]; then
                mkdir -p "$(dirname "$config_file")"
                printf '%s\n' "$selected" > "$config_file"
                echo "Pinned pr-review to GitHub account: $selected"
            fi
        fi
    fi
fi

if [ "$INSTALL_RDS" = true ]; then
    if ! command -v aws >/dev/null 2>&1; then
        offer_brew_install "AWS CLI" install awscli || exit 1
    fi
    if ! aws sts get-caller-identity >/dev/null 2>&1; then
        echo "Note: you are not signed in to AWS. The rds-load plugin will show a"
        echo "'Run aws sso login' prompt in its dropdown until you do."
    fi
fi

# ---- resolve source -------------------------------------------------------
# Resolve the directory containing this script (works for both local checkout
# and curl | bash, where BASH_SOURCE[0] is empty and $0 is just "bash").
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)

REPO_URL="https://github.com/ohlrogge/menu-bar-badges.git"

echo "Go $(go version | awk '{print $3}') found — building..."

# Use a local checkout when available; shallow-clone the repo otherwise. (The
# multi-binary layout spans subdirectories, so per-file curl no longer fits.)
if [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/go/go.mod" ]; then
    BUILD_DIR="$SCRIPT_DIR/go"
else
    CLONE_DIR=$(mktemp -d)
    trap 'rm -rf "$CLONE_DIR"' EXIT
    echo "Cloning source..."
    git clone --depth 1 "$REPO_URL" "$CLONE_DIR"
    BUILD_DIR="$CLONE_DIR/go"
fi

# Commit the binaries are built from, embedded so each plugin can check daily
# whether origin/main has moved past it (see internal/updatecheck). Works for
# both branches above since BUILD_DIR is always <repo root>/go.
REPO_ROOT="$(dirname "$BUILD_DIR")"
REPO_SHA="$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"

# ---- build ----------------------------------------------------------------
# Tell SwiftBar to exec the binary directly instead of wrapping it in
# "bash -l -c", which adds an unnecessary shell process on every refresh.
RUN_IN_BASH_TAG='<swiftbar.runInBash>false</swiftbar.runInBash>'

# build_plugin <pkg> <out> [extra swiftbar metadata tags…]: builds ./cmd/<pkg>
# into $PLUGIN_DIR/<out> and writes SwiftBar's binary-plugin metadata xattr
# (base64-encoded metadata tags — the only way to configure binary plugins,
# since they have no source comments SwiftBar can scan for <swiftbar.*> tags).
build_plugin() {
    local pkg="$1" out="$2" extra_meta="${3:-}"
    local binary="$PLUGIN_DIR/$out"
    # Remove stale binaries for this plugin (e.g. a previous refresh interval)
    # so SwiftBar doesn't run both the old and new file as duplicate badges.
    for stale in "$PLUGIN_DIR/$pkg".*.cgo; do
        [ -e "$stale" ] && [ "$stale" != "$binary" ] && rm -f "$stale"
    done
    (cd "$BUILD_DIR" && go build -ldflags "-X claude-quota/internal/updatecheck.BuiltSHA=$REPO_SHA" -o "$binary" "./cmd/$pkg")
    chmod +x "$binary"
    xattr -w "com.ameba.SwiftBar" "$(printf '%s%s' "$RUN_IN_BASH_TAG" "$extra_meta" | base64)" "$binary" 2>/dev/null || true
    echo "Installed $binary"
}

if [ "$INSTALL_CLAUDE" = true ]; then
    build_plugin claude-quota "claude-quota.1m.cgo"
fi
if [ "$INSTALL_GH" = true ]; then
    build_plugin pr-review "pr-review.1m.cgo"
fi
if [ "$INSTALL_RDS" = true ]; then
    # Window/refresh interval are configured from the plugin's own dropdown
    # (Window/Refresh submenus), which persist to ~/.config/rds-load/settings.
    # Deliberately no <swiftbar.environment> tag here: SwiftBar injects that
    # tag's declared value into every invocation unconditionally (see
    # Plugin.swift's `env` property), which would silently overwrite every
    # dropdown click with the install-time default on the very next refresh.
    build_plugin rds-load "rds-load.1m.cgo"
fi

open -a SwiftBar

# Add SwiftBar to Login Items so the menu bar items survive a reboot.
if ! osascript -e 'tell application "System Events" to get the name of every login item' 2>/dev/null | grep -q "SwiftBar"; then
    osascript -e 'tell application "System Events" to make login item at end with properties {path:"/Applications/SwiftBar.app", hidden:false}' >/dev/null 2>&1 \
        || echo "Could not add SwiftBar to Login Items — enable 'Launch at Login' in SwiftBar's preferences instead." >&2
fi

echo
if [ "$INSTALL_CLAUDE" = true ]; then
    echo "claude-quota: if macOS shows a Keychain dialog, click 'Always Allow' so the"
    echo "widget can refresh unattended."
fi
