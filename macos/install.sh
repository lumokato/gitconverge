#!/bin/bash
# User-only install/upgrade. Never modifies a managed repository or SSH settings.
set -euo pipefail
cd "$(dirname "$0")"
[[ $(uname -s) == Darwin && $(uname -m) == x86_64 ]] || { echo 'This package requires Intel macOS.' >&2; exit 1; }
app="$HOME/Applications/GitConverge.app"
config="$HOME/Library/Application Support/GitConverge/config.toml"
plist="$HOME/Library/LaunchAgents/com.gitconverge.menubar.plist"
domain="gui/$(id -u)"
# Quit UI explicitly before upgrade; never kill a user's active sync or daemon.
if pgrep -f "$app/Contents/MacOS/GitConverge" >/dev/null; then
 echo 'Quit GitConverge via its menu before upgrading (the worker continues).' >&2; exit 1
fi
if [[ ! -f "$config" && $# == 0 ]]; then
 echo 'First install: edit config.macos.example.toml, then ./install.sh /absolute/config.toml' >&2; exit 1
fi
source_config="${1:-$config}"
[[ -f "$source_config" ]] || { echo 'Configuration not found' >&2; exit 1; }
./GitConverge.app/Contents/MacOS/gitconverge-worker check --config "$source_config" >/dev/null
# Check worker lock before any launchd changes. CLI installer repeats under lock.
./GitConverge.app/Contents/MacOS/gitconverge-worker status --json | /usr/bin/python3 -c 'import json,sys; s=json.load(sys.stdin); sys.exit("Sync active; retry when idle" if s["runtime"]["syncActive"] else 0)'
backup="$HOME/Library/Application Support/GitConverge/desktop-backups/$(date +%Y%m%dT%H%M%S)-$$"
mkdir -p "$backup" "$HOME/Applications" "$(dirname "$plist")"
chmod 700 "$backup"
for path in "$app" "$plist" "$HOME/.local/bin/gitconverge" "$config" "$HOME/Library/LaunchAgents/com.gitconverge.worker.plist" "$HOME/Library/Caches/GitConverge"; do
 if [[ -L "$path" ]]; then echo "Refuse symlink: $path" >&2; exit 1; fi
 if [[ -e "$path" ]]; then ditto "$path" "$backup/$(basename "$path")"; fi
done
printf '%s\n' 'Restore only program/plists by default; state/config copies are for inspection, do not overwrite newer user data.' > "$backup/RESTORE.txt"
ui_loaded=false
if launchctl print "$domain/com.gitconverge.menubar" >/dev/null 2>&1; then ui_loaded=true; launchctl bootout "$domain/com.gitconverge.menubar"; fi
worker_updated=false
rollback() {
 trap - ERR
 echo "Desktop installation failed. Recovery copies: $backup" >&2
 # Shell owns ONLY UI rollback. Worker/config/plist rollback is exclusively
 # the Go installer's locked transaction. Never bootout or overwrite a worker
 # here: a sync may have started since either the initial check or install.
 launchctl bootout "$domain/com.gitconverge.menubar" >/dev/null 2>&1 || true
 if [[ -d "$app" ]]; then mv "$app" "$backup/failed-app"; fi
 if [[ -d "$backup/GitConverge.app" ]]; then ditto "$backup/GitConverge.app" "$app"; fi
 if [[ -f "$backup/com.gitconverge.menubar.plist" ]]; then
  cp -p "$backup/com.gitconverge.menubar.plist" "$plist"
 elif [[ -f "$plist" ]]; then
  mv "$plist" "$backup/failed-menubar.plist"
 fi
 if $ui_loaded; then launchctl bootstrap "$domain" "$plist" || true; fi
 if $worker_updated; then
  echo 'Worker upgrade succeeded and remains installed/running. Only UI was rolled back; retry installation after sync becomes idle.' >&2
 else
  echo 'Worker transaction did not complete; its own lock/rollback preserves the previous installation. Shell did not touch it.' >&2
 fi
}
trap rollback ERR
if [[ -d "$app" ]]; then mv "$app" "$backup/previous-live.app"; fi
ditto ./GitConverge.app "$app"
./GitConverge.app/Contents/MacOS/gitconverge-worker install --config "$source_config"
worker_updated=true
/usr/bin/python3 - "$app" "$plist" <<'PY'
import plistlib,sys,os
app,path=sys.argv[1:]
data={'Label':'com.gitconverge.menubar','ProgramArguments':[app+'/Contents/MacOS/GitConverge'],'RunAtLoad':True,'ProcessType':'Interactive','Umask':63,'StandardOutPath':'/dev/null','StandardErrorPath':'/dev/null'}
with open(path+'.new','wb') as f: plistlib.dump(data,f)
os.chmod(path+'.new',0o600); os.replace(path+'.new',path)
PY
launchctl bootstrap "$domain" "$plist"
trap - ERR
echo "Installed: $app"
echo "Desktop/worker backup: $backup"
echo 'Menu bar launch-at-login is separate; worker alone schedules sync. No Developer ID signature or notarization.'
