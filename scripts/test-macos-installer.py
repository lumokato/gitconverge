#!/usr/bin/env python3
"""Bounded shell control-flow fault injection; no real launchctl or user HOME.
Run with python3 scripts/test-macos-installer.py. Fixtures stay under the supplied
output directory (default repository dist/installer-tests, ignored by Git).
"""
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile

repo = pathlib.Path(__file__).resolve().parent.parent
output = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else repo / 'dist/installer-tests').resolve()
output.mkdir(parents=True, exist_ok=True)
for scenario in ['busy-after-check', 'ui-bootstrap-fails', 'success']:
    fixture = pathlib.Path(tempfile.mkdtemp(prefix=scenario+'-', dir=output))
    shutil.copyfile(repo / 'macos/install.sh', fixture / 'install.sh')
    home = fixture / 'home'
    paths = {
        'config': home / 'Library/Application Support/GitConverge/config.toml',
        'worker': home / '.local/bin/gitconverge',
        'worker-plist': home / 'Library/LaunchAgents/com.gitconverge.worker.plist',
        'ui-plist': home / 'Library/LaunchAgents/com.gitconverge.menubar.plist',
        'app': home / 'Applications/GitConverge.app/Contents/MacOS/GitConverge',
    }
    for name, path in paths.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('old-'+name+'\n')
    cache = home / 'Library/Caches/GitConverge'
    cache.mkdir(parents=True)
    (cache / 'state.json').write_text('original-state\n')
    worker = fixture / 'GitConverge.app/Contents/MacOS/gitconverge-worker'
    worker.parent.mkdir(parents=True)
    worker.write_text('''#!/bin/bash
case "$1" in
 check) exit 0;;
 status) echo '{"runtime":{"syncActive":false}}'; exit 0;;
 install)
  touch "$HOME/worker.active"
  if [[ "$SCENARIO" == busy-after-check ]]; then echo 'busy after idle check' >&2; exit 1; fi
  printf 'new-worker\\n' > "$HOME/.local/bin/gitconverge"
  printf 'new-config\\n' > "$HOME/Library/Application Support/GitConverge/config.toml"
  printf 'new-worker-plist\\n' > "$HOME/Library/LaunchAgents/com.gitconverge.worker.plist"
  exit 0;;
esac
exit 99
''')
    worker.chmod(0o755)
    (fixture / 'harness.sh').write_text('''#!/bin/bash
uname() { if [[ "$1" == -s ]]; then echo Darwin; else echo x86_64; fi; }
pgrep() { return 1; }
ditto() { cp -R "$1" "$2"; }
launchctl() {
 echo "launchctl $*" >> "$PROBE_LOG"
 case "$1" in
  print) return 0;;
  bootout) [[ "$2" != */com.gitconverge.worker ]] || echo UNSAFE >> "$PROBE_LOG"; return 0;;
  bootstrap)
   if [[ "$SCENARIO" == ui-bootstrap-fails && "$3" == *com.gitconverge.menubar.plist ]]; then return 5; fi
   return 0;;
 esac
 return 99
}
source ./install.sh
''')
    env = {**os.environ, 'HOME': str(home), 'SCENARIO': scenario, 'PROBE_LOG': str(fixture / 'probe.log')}
    result = subprocess.run(['/bin/bash', './harness.sh'], cwd=fixture, env=env, capture_output=True, text=True, timeout=15)
    (fixture / 'output.log').write_text(result.stdout+result.stderr)
    log = (fixture / 'probe.log').read_text()
    assert 'UNSAFE' not in log, log
    assert 'bootout gui/'+str(os.getuid())+'/com.gitconverge.worker' not in log
    assert (home / 'worker.active').exists(), 'rollback stopped active operation'
    assert (cache / 'state.json').read_text() == 'original-state\n'
    cache_backups = list(home.glob('Library/Application Support/GitConverge/desktop-backups/*/state-cache/state.json'))
    assert len(cache_backups) == 1 and cache_backups[0].read_text() == 'original-state\n', 'state cache backup missing'
    if scenario == 'busy-after-check':
        assert result.returncode != 0
        for name in ['config', 'worker', 'worker-plist', 'ui-plist', 'app']:
            assert paths[name].read_text() == 'old-'+name+'\n', name
    elif scenario == 'ui-bootstrap-fails':
        assert result.returncode != 0
        for name in ['config', 'worker', 'worker-plist']:
            assert paths[name].read_text() == 'new-'+name+'\n', name
        for name in ['ui-plist', 'app']:
            assert paths[name].read_text() == 'old-'+name+'\n', name
        assert 'Worker upgrade succeeded and remains installed/running' in result.stderr
    else:
        assert result.returncode == 0, result.stderr
        assert paths['worker'].read_text() == 'new-worker\n'
    print('PASS '+scenario+': no worker bootout/overwrite by outer rollback; exit='+str(result.returncode))
print('3/3 isolated installer control-flow cases passed; not real macOS launchd failure injection.')
