#!/usr/bin/env python3
"""Build a clean portable Windows archive; never include local user data."""
import argparse, hashlib, os, re, shutil, subprocess, sys, zipfile
from pathlib import Path
root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--go', default='go')
a = p.parse_args()
version = re.search(r'const Version = "([^"]+)"', (root/'internal/app/model.go').read_text()).group(1)
dist = root/'dist'
bundle = dist/('PortTide-'+version+'-windows-x64')
if bundle.exists():
    if bundle.is_symlink() or not bundle.resolve().is_relative_to(dist.resolve()):
        raise SystemExit('Unexpected output directory')
    shutil.rmtree(bundle)
bundle.mkdir(parents=True)
env = dict(os.environ, GOOS='windows', GOARCH='amd64', CGO_ENABLED='0')
subprocess.run([a.go,'build','-trimpath','-ldflags=-s -w -H=windowsgui','-o',str(bundle/'PortTide.exe'),'./cmd/porttide'],cwd=root,env=env,check=True)
core_cache=root/'.tools/xray-windows-package'
subprocess.run([sys.executable,str(root/'scripts/fetch-core.py'),'--platform','windows-amd64','--dest',str(core_cache)],check=True)
(bundle/'core').mkdir()
for name in ['xray.exe','geoip.dat','geosite.dat','LICENSE','SOURCE.txt']:
    shutil.copy2(core_cache/name,bundle/'core'/name)
# TUN libraries and vendor launch scripts are intentionally not packaged.
for name in ['README.md','README.ru.md','LICENSE','THIRD_PARTY_NOTICES.md','core-manifest.json']:
    shutil.copy2(root/name,bundle/name)
(bundle/'examples').mkdir()
shutil.copy2(root/'examples/settings.json',bundle/'examples/settings.json')
goroot=Path(subprocess.check_output([a.go,'env','GOROOT'],text=True).strip())
(bundle/'third_party').mkdir()
shutil.copy2(goroot/'LICENSE',bundle/'third_party/GO-LICENSE')
shutil.copy2(root/'internal/app/web/flags/LICENSE',bundle/'third_party/FLAG-ICONS-LICENSE')
archive=dist/(bundle.name+'.zip')
with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED,compresslevel=6) as z:
    for file in sorted(bundle.rglob('*')):
        if file.is_file(): z.write(file,Path(bundle.name)/file.relative_to(bundle))
checksum=hashlib.sha256(archive.read_bytes()).hexdigest()
archive.with_suffix('.zip.sha256').write_text(checksum+'  '+archive.name+'\n',encoding='ascii')
print(str(archive))
print('SHA-256: '+checksum)
