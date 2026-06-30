#!/usr/bin/env bash
# Removes the SwiftBar plugins and optionally the tools installed alongside them.
set -euo pipefail

if [ "$(uname)" != "Darwin" ]; then
    echo "macOS only." >&2
    exit 1
fi

# confirm: ask a yes/no question, default yes.
confirm() {
    local reply
    printf "%s [Y/n] " "$1"
    read -r reply </dev/tty
    case "$reply" in [Nn]*) return 1 ;; esac
    return 0
}

removed_plugins=0

# ---- locate plugin directory ------------------------------------------------
PLUGIN_DIR=$(defaults read com.ameba.SwiftBar PluginDirectory 2>/dev/null || true)
[ -z "$PLUGIN_DIR" ] && PLUGIN_DIR="$HOME/.swiftbar"

# ---- plugins ----------------------------------------------------------------
echo "Checking for installed plugins in $PLUGIN_DIR..."
echo

for pkg in "claude-quota" "pr-review"; do
    found=()
    for f in "$PLUGIN_DIR/$pkg".*.cgo; do
        [ -e "$f" ] && found+=("$f")
    done
    if [ ${#found[@]} -gt 0 ]; then
        if confirm "Remove $pkg plugin (${found[*]})?"; then
            rm -f "${found[@]}"
            echo "Removed $pkg."
            removed_plugins=$((removed_plugins + 1))
        fi
    fi
done

# ---- config and cache -------------------------------------------------------
echo

if [ -f "$HOME/.config/pr-review/user" ]; then
    if confirm "Remove pr-review account config (~/.config/pr-review/user)?"; then
        rm -f "$HOME/.config/pr-review/user"
        rmdir "$HOME/.config/pr-review" 2>/dev/null || true
        echo "Removed pr-review config."
    fi
fi

if [ -d "$HOME/.cache/pr-review" ]; then
    if confirm "Remove pr-review cache (~/.cache/pr-review/)?"; then
        rm -rf "$HOME/.cache/pr-review"
        echo "Removed pr-review cache."
    fi
fi

if [ -d "$HOME/.cache/claude-quota" ]; then
    if confirm "Remove claude-quota cache (~/.cache/claude-quota/)?"; then
        rm -rf "$HOME/.cache/claude-quota"
        echo "Removed claude-quota cache."
    fi
fi

# ---- SwiftBar login item ----------------------------------------------------
echo

if osascript -e 'tell application "System Events" to get the name of every login item' 2>/dev/null | grep -q "SwiftBar"; then
    if confirm "Remove SwiftBar from Login Items?"; then
        osascript -e 'tell application "System Events" to delete login item "SwiftBar"' >/dev/null 2>&1 || true
        echo "Removed SwiftBar from Login Items."
    fi
fi

# ---- tools (brew) -----------------------------------------------------------
echo

if command -v brew >/dev/null 2>&1; then
    if command -v gh >/dev/null 2>&1 && brew list gh >/dev/null 2>&1; then
        if confirm "Uninstall GitHub CLI (gh) via Homebrew?"; then
            brew uninstall gh
        fi
    fi

    if [ -d "/Applications/SwiftBar.app" ] && brew list --cask swiftbar >/dev/null 2>&1; then
        if confirm "Uninstall SwiftBar via Homebrew?"; then
            brew uninstall --cask swiftbar
        fi
    fi

    if command -v go >/dev/null 2>&1 && brew list go >/dev/null 2>&1; then
        if confirm "Uninstall Go via Homebrew? (skip if you use Go for other projects)"; then
            brew uninstall go
        fi
    fi
fi

# ---- done -------------------------------------------------------------------
echo
if [ "$removed_plugins" -gt 0 ]; then
    echo "Done. Restart SwiftBar (or quit and reopen it) for the menu bar to update."
else
    echo "No plugins were removed."
fi
