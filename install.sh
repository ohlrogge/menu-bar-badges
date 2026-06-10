#!/usr/bin/env bash
# Installs the Claude Quota SwiftBar plugin.
set -euo pipefail

if [ "$(uname)" != "Darwin" ]; then
    echo "macOS only." >&2
    exit 1
fi

if [ ! -d "/Applications/SwiftBar.app" ]; then
    if ! command -v brew >/dev/null; then
        echo "Homebrew is required to install SwiftBar: https://brew.sh" >&2
        exit 1
    fi
    echo "Installing SwiftBar..."
    brew install --cask swiftbar
fi

PLUGIN_DIR=$(defaults read com.ameba.SwiftBar PluginDirectory 2>/dev/null || true)
if [ -z "$PLUGIN_DIR" ]; then
    PLUGIN_DIR="$HOME/.swiftbar"
    defaults write com.ameba.SwiftBar PluginDirectory -string "$PLUGIN_DIR"
fi
mkdir -p "$PLUGIN_DIR"

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cp "$SCRIPT_DIR/claude-quota.5m.py" "$PLUGIN_DIR/"
chmod +x "$PLUGIN_DIR/claude-quota.5m.py"

open -a SwiftBar
echo
echo "Installed to $PLUGIN_DIR/claude-quota.5m.py"
echo "If macOS shows a Keychain dialog, click 'Always Allow' so the widget"
echo "can refresh unattended."
