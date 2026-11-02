#!/usr/bin/env python3
"""Compile a per-user installer from an already prepared clean bundle (Windows)."""
import argparse, hashlib, re, shutil, subprocess
from pathlib import Path
root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--bundle', type=Path)
p.add_argument('--iscc', default=shutil.which('ISCC') or r'C:\Program Files (x86)\Inno Setup 6\ISCC.exe')
p.add_argument('--output', type=Path, default=root/'dist')
p.add_argument('--test-data-dir', help='Compile an isolated smoke-test installer, never a release')
a = p.parse_args()
version = re.search(r'const Version = "([^"]+)"', (root/'internal/app/model.go').read_text()).group(1)
if a.bundle is None:
    a.bundle = root/'dist'/('PortTide-'+version+'-windows-x64')
if not (a.bundle/'PortTide.exe').is_file():
    raise SystemExit('Prepare a clean bundle with scripts/package.py first')
a.output.mkdir(parents=True, exist_ok=True)
args = [a.iscc, '/Qp', '/DAppVersion='+version, '/DBundleDir='+str(a.bundle.resolve()), '/DOutputDir='+str(a.output.resolve())]
if a.test_data_dir:
    args += ['/DTestBuild=1', '/DTestDataDir='+str(Path(a.test_data_dir).resolve())]
subprocess.run(args+[str(root/'scripts/installer.iss')], check=True)
setup = a.output/('PortTide-'+version+'-windows-x64-setup.exe')
setup.with_suffix('.exe.sha256').write_text(hashlib.sha256(setup.read_bytes()).hexdigest()+'  '+setup.name+'\n', encoding='ascii')
print(setup)
