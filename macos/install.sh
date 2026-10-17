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
./GitConverge.app/Contents/MacOS/gitconverge check --config "$source_config" >/dev/null
# Check worker lock before any launchd changes. CLI installer repeats under lock.
./GitConverge.app/Contents/MacOS/gitconverge status --json | /usr/bin/python3 -c 'import json,sys; s=json.load(sys.stdin); sys.exit("Sync active; retry when idle" if s["runtime"]["syncActive"] else 0)'
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
worker_loaded=false
if launchctl print "$domain/com.gitconverge.worker" >/dev/null 2>&1; then worker_loaded=true; fi
rollback() {
 trap - ERR
 echo "Installation failed. Recovery copies: $backup" >&2
 launchctl bootout "$domain/com.gitconverge.menubar" >/dev/null 2>&1 || true
 launchctl bootout "$domain/com.gitconverge.worker" >/dev/null 2>&1 || true
 if [[ -d "$app" ]]; then mv "$app" "$backup/failed-app"; fi
 if [[ -d "$backup/GitConverge.app" ]]; then ditto "$backup/GitConverge.app" "$app"; fi
 for name in gitconverge config.toml com.gitconverge.worker.plist com.gitconverge.menubar.plist; do
  case "$name" in
   gitconverge) target="$HOME/.local/bin/gitconverge";;
   config.toml) target="$config";;
   *) target="$HOME/Library/LaunchAgents/$name";;
  esac
  if [[ -f "$backup/$name" ]]; then cp -p "$backup/$name" "$target";
  elif [[ -f "$target" ]]; then mv "$target" "$backup/failed-$name"; fi
 done
 if $worker_loaded; then launchctl bootstrap "$domain" "$HOME/Library/LaunchAgents/com.gitconverge.worker.plist" || true; fi
 if $ui_loaded; then launchctl bootstrap "$domain" "$plist" || true; fi
}
trap rollback ERR
if [[ -d "$app" ]]; then mv "$app" "$backup/previous-live.app"; fi
ditto ./GitConverge.app "$app"
./GitConverge.app/Contents/MacOS/gitconverge install --config "$source_config"
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
