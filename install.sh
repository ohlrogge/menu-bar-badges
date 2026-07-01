#!/usr/bin/env bash
# Installs the SwiftBar plugins from this repo (compiled Go binaries).
#
# Plugins:
#   claude-quota  — Claude Code usage gauges in the menu bar
#   pr-review     — GitHub PRs awaiting your review (needs the gh CLI)
#
# Choose what to install interactively, or non-interactively with either
# flags (--claude / --gh / --all) or the PLUGINS env var (e.g. PLUGINS=claude,gh).
set -euo pipefail

if [ "$(uname)" != "Darwin" ]; then
    echo "macOS only." >&2
    exit 1
fi

# ---- choose plugins -------------------------------------------------------
INSTALL_CLAUDE=false
INSTALL_GH=false

for arg in "$@"; do
    case "$arg" in
        --claude) INSTALL_CLAUDE=true ;;
        --gh|--pr|--pr-review) INSTALL_GH=true ;;
        --all|--both) INSTALL_CLAUDE=true; INSTALL_GH=true ;;
    esac
done

if [ -n "${PLUGINS:-}" ]; then
    case ",$PLUGINS," in *,claude,*|*,claude-quota,*) INSTALL_CLAUDE=true ;; esac
    case ",$PLUGINS," in *,gh,*|*,pr,*|*,pr-review,*)  INSTALL_GH=true ;; esac
fi

if [ "$INSTALL_CLAUDE" = false ] && [ "$INSTALL_GH" = false ]; then
    if [ -e /dev/tty ]; then
        echo "Which plugins do you want to install?"
        echo "  1) claude-quota  — Claude Code usage gauges"
        echo "  2) pr-review     — GitHub PRs awaiting your review"
        echo "  3) both (default)"
        printf "Choice [3]: "
        read -r choice </dev/tty
        case "$choice" in
            1) INSTALL_CLAUDE=true ;;
            2) INSTALL_GH=true ;;
            *) INSTALL_CLAUDE=true; INSTALL_GH=true ;;
        esac
    else
        echo "No selection given; installing both (use PLUGINS=claude or PLUGINS=gh to choose)."
        INSTALL_CLAUDE=true; INSTALL_GH=true
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

PLUGIN_DIR=$(defaults read com.ameba.SwiftBar PluginDirectory 2>/dev/null || true)
if [ -z "$PLUGIN_DIR" ]; then
    PLUGIN_DIR="$HOME/.swiftbar"
    defaults write com.ameba.SwiftBar PluginDirectory -string "$PLUGIN_DIR"
fi
mkdir -p "$PLUGIN_DIR"

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

# ---- build ----------------------------------------------------------------
# Tell SwiftBar to exec the binary directly instead of wrapping it in
# "bash -l -c", which adds an unnecessary shell process on every refresh.
RUN_IN_BASH=$(printf '<swiftbar.runInBash>false</swiftbar.runInBash>' | base64)

build_plugin() {
    local pkg="$1" out="$2"
    local binary="$PLUGIN_DIR/$out"
    # Remove stale binaries for this plugin (e.g. a previous refresh interval)
    # so SwiftBar doesn't run both the old and new file as duplicate badges.
    for stale in "$PLUGIN_DIR/$pkg".*.cgo; do
        [ -e "$stale" ] && [ "$stale" != "$binary" ] && rm -f "$stale"
    done
    (cd "$BUILD_DIR" && go build -o "$binary" "./cmd/$pkg")
    chmod +x "$binary"
    xattr -w "com.ameba.SwiftBar" "$RUN_IN_BASH" "$binary" 2>/dev/null || true
    echo "Installed $binary"
}

if [ "$INSTALL_CLAUDE" = true ]; then
    build_plugin claude-quota "claude-quota.1m.cgo"
fi
if [ "$INSTALL_GH" = true ]; then
    build_plugin pr-review "pr-review.1m.cgo"
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
