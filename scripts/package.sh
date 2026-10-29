#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/build/NearTransfer.app"
NAME="NearTransfer-0.1.0-dev-$(uname -m)-$(TZ=Asia/Shanghai date +%Y%m%d-%H%M%S)"
codesign --verify --deep --strict "$APP"
python3 - "$ROOT" "$NAME" <<'PY'
import pathlib,stat,sys,zipfile,hashlib
root=pathlib.Path(sys.argv[1]);name=sys.argv[2];app=root/'build/NearTransfer.app'
archive=root/'build'/f'{name}.zip';extracted=root/'build'/f'{name}-extracted'
with zipfile.ZipFile(archive,'x',compression=zipfile.ZIP_DEFLATED) as z:
    for p in sorted(app.rglob('*')):
        if p.name.startswith('._') or p.name=='.DS_Store':continue
        if p.is_symlink():raise RuntimeError('Unexpected symlink; packaging contract needs review')
        if p.is_file():z.write(p,p.relative_to(app.parent))
with zipfile.ZipFile(archive) as z:
    for info in z.infolist():
        z.extract(info,extracted)
        (extracted/info.filename).chmod((info.external_attr >> 16)&0o777)
for p in app.rglob('*'):
    if p.is_file() and not p.name.startswith('._') and p.name!='.DS_Store':
        q=extracted/p.relative_to(app.parent)
        assert p.read_bytes()==q.read_bytes(),str(p)
        assert stat.S_IMODE(p.stat().st_mode)==stat.S_IMODE(q.stat().st_mode),str(p)
print(archive)
print('SHA256',hashlib.sha256(archive.read_bytes()).hexdigest())
PY
codesign --verify --deep --strict "$ROOT/build/$NAME-extracted/NearTransfer.app"
