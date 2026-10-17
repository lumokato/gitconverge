#!/bin/bash
# Explicitly invoked uninstall; deliberately preserves user repositories/config/state/backups.
set -euo pipefail
app="$HOME/Applications/GitConverge.app"
plist="$HOME/Library/LaunchAgents/com.gitconverge.menubar.plist"
domain="gui/$(id -u)"
if pgrep -f "$app/Contents/MacOS/GitConverge" >/dev/null; then echo 'Quit the menu bar first.' >&2; exit 1; fi
worker="$HOME/.local/bin/gitconverge"
[[ ! -L "$app" && ! -L "$plist" ]] || { echo 'Refusing symlinks' >&2; exit 1; }
# Worker uninstall refuses while a sync is active, and takes a recovery snapshot.
if [[ -x "$worker" ]]; then "$worker" uninstall; fi
if launchctl print "$domain/com.gitconverge.menubar" >/dev/null 2>&1; then launchctl bootout "$domain/com.gitconverge.menubar"; fi
backup="$HOME/Library/Application Support/GitConverge/desktop-backups/uninstall-$(date +%Y%m%dT%H%M%S)-$$"
mkdir -p "$backup"; chmod 700 "$backup"
# Move instead of deleting the app, retaining an executable recovery point.
if [[ -d "$app" ]]; then mv "$app" "$backup/"; fi
if [[ -f "$plist" ]]; then mv "$plist" "$backup/"; fi
echo "Uninstalled. App recovery copy: $backup"
echo 'Configuration, pause preference, state, logs, SSH settings, backups and all repositories are untouched.'
