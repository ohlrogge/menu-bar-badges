#!/usr/bin/env python3
# <xbar.title>Claude Quota</xbar.title>
# <xbar.version>v1.1</xbar.version>
# <xbar.desc>Battery-style menu bar gauges for Claude Code quota, per account.</xbar.desc>
# <xbar.abouturl>https://github.com/grzegorz-raczek-unit8/claude-quota</xbar.abouturl>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideLastUpdated>false</swiftbar.hideLastUpdated>
#
# Reads Claude Code OAuth tokens from the macOS Keychain (read-only; never
# refreshes or rewrites them) and queries the same usage endpoint that
# Claude Code's /usage screen uses.
#
# Accounts are auto-discovered from ~/.claude and ~/.claude-* config dirs
# that have a Keychain entry. To pin/label accounts explicitly, create
# ~/.config/claude-quota/accounts with one entry per line:
#
#     ~/.claude-work Work
#     ~/.claude-priv Priv
#
# (path, then an optional label — the pill shows the label's first letter).
#
# Keychain entry name: "Claude Code-credentials" for ~/.claude, otherwise
# "Claude Code-credentials-<first 8 hex of sha256(config dir path)>".

import base64
import glob
import hashlib
import json
import os
import struct
import subprocess
import time
import urllib.request
import urllib.error
import zlib
from datetime import datetime

USAGE_URL = "https://api.anthropic.com/api/oauth/usage"
ACCOUNTS_FILE = os.path.expanduser("~/.config/claude-quota/accounts")


# ---- account discovery ----

def keychain_service(config_dir):
    if os.path.basename(config_dir) == ".claude":
        return "Claude Code-credentials"
    digest = hashlib.sha256(config_dir.encode()).hexdigest()[:8]
    return f"Claude Code-credentials-{digest}"


def keychain_entry_exists(service):
    out = subprocess.run(
        ["security", "find-generic-password", "-s", service],
        capture_output=True, timeout=10,
    )
    return out.returncode == 0


def default_label(config_dir):
    name = os.path.basename(config_dir)
    if name == ".claude":
        return "Default"
    return name.removeprefix(".claude-").capitalize() or "Default"


def discover_accounts():
    """Return [(label, config_dir)]. Pinned file wins over auto-discovery."""
    accounts = []
    if os.path.exists(ACCOUNTS_FILE):
        with open(ACCOUNTS_FILE) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                parts = line.split(None, 1)
                path = os.path.abspath(os.path.expanduser(parts[0]))
                label = parts[1].strip() if len(parts) > 1 else default_label(path)
                accounts.append((label, path))
        return accounts
    for path in sorted(glob.glob(os.path.expanduser("~/.claude*"))):
        if os.path.isdir(path) and keychain_entry_exists(keychain_service(path)):
            accounts.append((default_label(path), path))
    return accounts


# ---- data fetching ----

def get_token(config_dir):
    """Return (token, error). Reads the Claude Code keychain entry."""
    try:
        out = subprocess.run(
            ["security", "find-generic-password",
             "-s", keychain_service(config_dir), "-w"],
            capture_output=True, text=True, timeout=10,
        )
    except subprocess.TimeoutExpired:
        return None, "keychain timeout"
    if out.returncode != 0:
        return None, "no keychain entry — log in with that CLI once"
    try:
        creds = json.loads(out.stdout)["claudeAiOauth"]
    except (json.JSONDecodeError, KeyError):
        return None, "unexpected credential format"
    expires_at = creds.get("expiresAt")
    if expires_at and expires_at / 1000 < time.time():
        return None, "token stale — run that CLI once to refresh"
    return creds.get("accessToken"), None


def fetch_usage(token):
    """Return (usage dict, error)."""
    req = urllib.request.Request(USAGE_URL, headers={
        "Authorization": f"Bearer {token}",
        "anthropic-beta": "oauth-2025-04-20",
        "Content-Type": "application/json",
    })
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            return json.loads(resp.read()), None
    except urllib.error.HTTPError as e:
        if e.code == 401:
            return None, "token rejected — run that CLI once to refresh"
        return None, f"API error {e.code}"
    except Exception:
        return None, "offline?"


# ---- menu bar image (battery pills as a retina PNG) ----
# Drawn at 2x pixels with a 144-dpi pHYs chunk so macOS shows it at half
# size, crisp on retina.

OUTLINE = (255, 255, 255, 255)
ERR_OUTLINE = (255, 159, 10, 255)
GREEN = (52, 199, 89, 255)
ORANGE = (255, 159, 10, 255)
RED = (255, 59, 48, 255)

# 5x7 pixel font, drawn at 2x scale (each cell = 2x2 px)
GLYPHS = {
    "A": [".XXX.", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"],
    "B": ["XXXX.", "X...X", "X...X", "XXXX.", "X...X", "X...X", "XXXX."],
    "C": [".XXX.", "X...X", "X....", "X....", "X....", "X...X", ".XXX."],
    "D": ["XXXX.", "X...X", "X...X", "X...X", "X...X", "X...X", "XXXX."],
    "E": ["XXXXX", "X....", "X....", "XXXX.", "X....", "X....", "XXXXX"],
    "F": ["XXXXX", "X....", "X....", "XXXX.", "X....", "X....", "X...."],
    "G": [".XXX.", "X...X", "X....", "X.XXX", "X...X", "X...X", ".XXX."],
    "H": ["X...X", "X...X", "X...X", "XXXXX", "X...X", "X...X", "X...X"],
    "I": [".XXX.", "..X..", "..X..", "..X..", "..X..", "..X..", ".XXX."],
    "J": ["..XXX", "...X.", "...X.", "...X.", "...X.", "X..X.", ".XX.."],
    "K": ["X...X", "X..X.", "X.X..", "XX...", "X.X..", "X..X.", "X...X"],
    "L": ["X....", "X....", "X....", "X....", "X....", "X....", "XXXXX"],
    "M": ["X...X", "XX.XX", "X.X.X", "X.X.X", "X...X", "X...X", "X...X"],
    "N": ["X...X", "XX..X", "X.X.X", "X..XX", "X...X", "X...X", "X...X"],
    "O": [".XXX.", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."],
    "P": ["XXXX.", "X...X", "X...X", "XXXX.", "X....", "X....", "X...."],
    "Q": [".XXX.", "X...X", "X...X", "X...X", "X.X.X", "X..X.", ".XX.X"],
    "R": ["XXXX.", "X...X", "X...X", "XXXX.", "X.X..", "X..X.", "X...X"],
    "S": [".XXXX", "X....", "X....", ".XXX.", "....X", "....X", "XXXX."],
    "T": ["XXXXX", "..X..", "..X..", "..X..", "..X..", "..X..", "..X.."],
    "U": ["X...X", "X...X", "X...X", "X...X", "X...X", "X...X", ".XXX."],
    "V": ["X...X", "X...X", "X...X", "X...X", "X...X", ".X.X.", "..X.."],
    "W": ["X...X", "X...X", "X...X", "X.X.X", "X.X.X", "X.X.X", ".X.X."],
    "X": ["X...X", "X...X", ".X.X.", "..X..", ".X.X.", "X...X", "X...X"],
    "Y": ["X...X", "X...X", ".X.X.", "..X..", "..X..", "..X..", "..X.."],
    "Z": ["XXXXX", "....X", "...X.", "..X..", ".X...", "X....", "XXXXX"],
    "0": [".XXX.", "X...X", "X..XX", "X.X.X", "XX..X", "X...X", ".XXX."],
    "1": ["..X..", ".XX..", "..X..", "..X..", "..X..", "..X..", ".XXX."],
    "2": [".XXX.", "X...X", "....X", "...X.", "..X..", ".X...", "XXXXX"],
    "3": [".XXX.", "X...X", "....X", "..XX.", "....X", "X...X", ".XXX."],
    "4": ["...X.", "..XX.", ".X.X.", "X..X.", "XXXXX", "...X.", "...X."],
    "5": ["XXXXX", "X....", "XXXX.", "....X", "....X", "X...X", ".XXX."],
    "6": [".XXX.", "X....", "XXXX.", "X...X", "X...X", "X...X", ".XXX."],
    "7": ["XXXXX", "....X", "...X.", "..X..", "..X..", "..X..", "..X.."],
    "8": [".XXX.", "X...X", "X...X", ".XXX.", "X...X", "X...X", ".XXX."],
    "9": [".XXX.", "X...X", "X...X", ".XXXX", "....X", "....X", ".XXX."],
    ":": [".....", "..X..", "..X..", ".....", "..X..", "..X..", "....."],
}


def encode_png(width, height, pixels):
    raw = b"".join(
        b"\x00" + b"".join(struct.pack("4B", *px) for px in row)
        for row in pixels
    )

    def chunk(typ, data):
        body = typ + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body))

    ihdr = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
    phys = struct.pack(">IIB", 5669, 5669, 1)  # 144 dpi -> 2x scale
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"pHYs", phys)
            + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b""))


def fill_rect(pixels, x0, y0, x1, y1, color):
    for y in range(y0, y1):
        for x in range(x0, x1):
            pixels[y][x] = color


def draw_letter(pixels, x0, y0, ch, color, scale=2):
    for r, row in enumerate(GLYPHS.get(ch.upper(), GLYPHS["I"])):
        for c, v in enumerate(row):
            if v == "X":
                fill_rect(pixels, x0 + c * scale, y0 + r * scale,
                          x0 + (c + 1) * scale, y0 + (r + 1) * scale, color)


def draw_battery(pixels, x, y, utilization, error, text=None):
    """One battery pill: body 66x24 px + nub, at (x, y)."""
    outline = ERR_OUTLINE if error else OUTLINE
    body_w, body_h, t = 66, 24, 2
    # body outline
    fill_rect(pixels, x, y, x + body_w, y + t, outline)
    fill_rect(pixels, x, y + body_h - t, x + body_w, y + body_h, outline)
    fill_rect(pixels, x, y, x + t, y + body_h, outline)
    fill_rect(pixels, x + body_w - t, y, x + body_w, y + body_h, outline)
    # soften corners
    for cx, cy in [(x, y), (x + body_w - 1, y),
                   (x, y + body_h - 1), (x + body_w - 1, y + body_h - 1)]:
        pixels[cy][cx] = (0, 0, 0, 0)
    # nub (battery tip)
    fill_rect(pixels, x + body_w + 1, y + 7, x + body_w + 5, y + body_h - 7, outline)
    if error or utilization is None:
        return
    # fill, battery-style: color shifts as the window fills up
    color = RED if utilization >= 90 else ORANGE if utilization >= 70 else GREEN
    inner_w = body_w - 2 * t - 2
    fill_w = round(inner_w * min(100, max(0, utilization)) / 100)
    if utilization > 0 and fill_w == 0:
        fill_w = 1
    fill_rect(pixels, x + t + 1, y + t + 1,
              x + t + 1 + fill_w, y + body_h - t - 1, color)
    # exact percentage (or countdown override), centered inside the pill
    if text is None:
        text = f"{utilization:.0f}"
    text_w = len(text) * 10 + (len(text) - 1) * 2
    tx = x + (body_w - text_w) // 2
    for ch in text:
        draw_letter(pixels, tx, y + (body_h - 14) // 2, ch, OUTLINE)
        tx += 12


def menu_bar_image(results):
    letter_w, gap = 10, 4
    cell_w = letter_w + gap + 66 + 5  # letter + gap + body + nub
    n = len(results)
    width, height = n * cell_w + (n - 1) * 12, 32
    pixels = [[(0, 0, 0, 0)] * width for _ in range(height)]
    for i, (name, usage, err) in enumerate(results):
        x = i * (cell_w + 12)
        five = (usage or {}).get("five_hour")
        text = None
        if five and five["utilization"] >= 100 and five.get("resets_at"):
            # quota exhausted: show time until reset instead of "100"
            try:
                left = datetime.fromisoformat(five["resets_at"]) \
                    - datetime.now().astimezone()
                mins = max(0, int(left.total_seconds()) // 60)
                text = f"{mins // 60}:{mins % 60:02d}"
            except (ValueError, TypeError):
                pass
        draw_letter(pixels, x, 9, name[0], OUTLINE)
        draw_battery(pixels, x + letter_w + gap, 4,
                     utilization=five["utilization"] if five else None,
                     error=bool(err), text=text)
    return base64.b64encode(encode_png(width, height, pixels)).decode()


# ---- dropdown rendering ----

def meter(utilization):
    filled = min(10, max(0, round(utilization / 10)))
    return "█" * filled + "░" * (10 - filled)


def reset_str(iso):
    try:
        local = datetime.fromisoformat(iso).astimezone()
        day = "today" if local.date() == datetime.now().astimezone().date() \
            else local.strftime("%a")
        return f"resets {day} {local.strftime('%H:%M')}"
    except (ValueError, TypeError):
        return ""


def color(utilization):
    if utilization >= 90:
        return " | color=red"
    if utilization >= 70:
        return " | color=orange"
    return ""


def window_line(label, window):
    u = window["utilization"]
    return (f"--{label:<7} {meter(u)} {u:.0f}%  "
            f"{reset_str(window.get('resets_at'))}{color(u)} | font=Menlo")


def main():
    accounts = discover_accounts()
    if not accounts:
        print("◔ ? | color=orange")
        print("---")
        print("No Claude accounts found")
        print("Log in with the claude CLI once, or pin accounts in "
              "~/.config/claude-quota/accounts")
        return

    results = []  # (label, usage-or-None, error-or-None)
    for label, config_dir in accounts:
        token, err = get_token(config_dir)
        usage = None
        if not err:
            usage, err = fetch_usage(token)
        results.append((label, usage, err))

    try:
        print(f"| image={menu_bar_image(results)}")
    except Exception:
        # fallback: plain text if rendering ever breaks
        parts = []
        for name, usage, err in results:
            five = (usage or {}).get("five_hour")
            parts.append(f"{name[0]} {five['utilization']:.0f}" if five
                         else f"{name[0]} ⚠")
        print(f"◔ {' · '.join(parts)}")

    print("---")
    for name, usage, err in results:
        if err:
            print(f"{name}: ⚠ {err} | color=orange")
            continue
        five, week = usage.get("five_hour"), usage.get("seven_day")
        summary = []
        if five:
            summary.append(f"5h {five['utilization']:.0f}%")
        if week:
            summary.append(f"wk {week['utilization']:.0f}%")
        print(f"{name} — {' · '.join(summary) or 'no quota data'}")
        if five:
            print(window_line("5-hour", five))
        if week:
            print(window_line("week", week))
        for key, label in [("seven_day_opus", "opus"), ("seven_day_sonnet", "sonnet")]:
            if usage.get(key):
                print(window_line(label, usage[key]))
        extra = usage.get("extra_usage") or {}
        if extra.get("is_enabled") and extra.get("used_credits"):
            # API reports credits in cents
            print(f"--extra   {extra['used_credits'] / 100:.2f} / "
                  f"{extra['monthly_limit'] / 100:.0f} "
                  f"{extra.get('currency', '')} | font=Menlo")
    print("---")
    print("Refresh now | refresh=true")


if __name__ == "__main__":
    main()
