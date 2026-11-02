#!/usr/bin/env python3
"""Fetch a pinned official Xray archive and verify its SHA-256 before extraction."""
import argparse, hashlib, io, json, os, urllib.request, zipfile
from pathlib import Path
root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--platform', choices=['linux-amd64', 'windows-amd64'], required=True)
p.add_argument('--dest', type=Path, default=root / 'core')
a = p.parse_args()
manifest = json.loads((root / 'core-manifest.json').read_text())
asset = manifest['assets'][a.platform]
url = 'https://github.com/XTLS/Xray-core/releases/download/v' + manifest['version'] + '/' + asset['file']
with urllib.request.urlopen(url, timeout=60) as response:
    blob = response.read(50 * 1024 * 1024 + 1)
if len(blob) > 50 * 1024 * 1024 or hashlib.sha256(blob).hexdigest() != asset['sha256']:
    raise SystemExit('Xray archive size/hash verification failed')
a.dest.mkdir(parents=True, exist_ok=True)
dest = a.dest.resolve()
with zipfile.ZipFile(io.BytesIO(blob)) as archive:
    for entry in archive.infolist():
        target = (dest / entry.filename).resolve()
        if not target.is_relative_to(dest):
            raise SystemExit('Unsafe archive member')
    archive.extractall(dest)
if a.platform == 'linux-amd64':
    (dest / 'xray').chmod(0o755)
(dest / 'SOURCE.txt').write_text('Xray ' + manifest['version'] + '\nMPL-2.0\nCorresponding source: ' + manifest['source'] + '\nArchive SHA-256: ' + asset['sha256'] + '\n', encoding='utf-8')
print('Verified Xray ' + manifest['version'] + ' for ' + a.platform)
