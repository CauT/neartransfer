#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$ROOT/build/NearTransfer.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" "$ROOT/build/go-cache" "$ROOT/build/swift-cache"
(cd "$ROOT/engine" && GOCACHE="$ROOT/build/go-cache" go build -trimpath -o "$APP/Contents/Resources/neartransfer-engine" .)
swiftc -swift-version 5 -O -module-cache-path "$ROOT/build/swift-cache" -target "$(uname -m)-apple-macosx13.0" -framework AppKit -framework CoreImage "$ROOT/mac/main.swift" -o "$APP/Contents/MacOS/NearTransfer"
cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>NearTransfer</string>
<key>CFBundleIdentifier</key><string>local.neartransfer.mac</string>
<key>CFBundleName</key><string>NearTransfer</string>
<key>CFBundleDisplayName</key><string>近传</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>0.1.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>NSHighResolutionCapable</key><true/>
<key>NSLocalNetworkUsageDescription</key><string>近传让同一局域网的设备直接与这台 Mac 互传文字、图片和文件。</string>
</dict></plist>
PLIST
cp "$ROOT/LICENSE" "$APP/Contents/Resources/LICENSE.txt"
python3 - "$ROOT" "$APP" <<'PROVENANCE'
import hashlib,json,pathlib,subprocess,sys
root=pathlib.Path(sys.argv[1]); app=pathlib.Path(sys.argv[2])
paths=[root/'mac/main.swift',root/'engine/main.go',root/'engine/go.mod',*sorted((root/'engine/web').glob('*')),root/'scripts/build.sh']
git_root=subprocess.run(['git','rev-parse','--show-toplevel'],cwd=root,text=True,capture_output=True)
commit=None; dirty=None
if git_root.returncode==0:
    repo=pathlib.Path(git_root.stdout.strip())
    source_path=str(root.relative_to(repo))
    commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
    dirty=subprocess.check_output(['git','status','--porcelain','--',source_path],cwd=repo,text=True).splitlines()
record={'gitCommit':commit,'sourceSHA256':{str(p.relative_to(root)):hashlib.sha256(p.read_bytes()).hexdigest() for p in paths},'dirtyInputs':dirty}
(app/'Contents/Resources/build-provenance.json').write_text(json.dumps(record,indent=2)+'\n')
PROVENANCE
codesign --force --sign - "$APP/Contents/Resources/neartransfer-engine"
codesign --force --sign - "$APP"
codesign --verify --deep --strict "$APP"
printf '%s\n' "$APP"
