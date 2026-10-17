#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
version="${1:?usage: build-macos-release.sh VERSION}"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z]+)*$ ]] || { echo 'Invalid version' >&2; exit 1; }
[[ "$(uname -s)" == Darwin && "$(uname -m)" == x86_64 ]] || { echo 'Native Intel macOS build required; arm64 is not validated.' >&2; exit 1; }
commit=$(git rev-parse --short=12 HEAD)
if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
  echo 'Refusing release from a dirty tree' >&2; exit 1
fi
export MACOSX_DEPLOYMENT_TARGET=12.0
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
go test ./...
go test -race ./...
go vet ./...
name="GitConverge-${version}-macos-amd64"
root="dist/$name"
[[ ! -e "$root" ]] || { echo "Build output already exists: $root" >&2; exit 1; }
app="$root/GitConverge.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
go build -trimpath -ldflags "-X main.version=${version}+${commit}" -o "$app/Contents/MacOS/gitconverge" ./cmd/gitconverge
swiftc -O -target x86_64-apple-macos12.0 -framework AppKit -framework WebKit macos/GitConverge.swift -o "$app/Contents/MacOS/GitConverge"
/usr/libexec/PlistBuddy -c 'Clear dict' "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleIdentifier string com.gitconverge.menubar' -c 'Add :CFBundleExecutable string GitConverge' -c 'Add :CFBundleName string GitConverge' -c 'Add :CFBundlePackageType string APPL' -c "Add :CFBundleShortVersionString string $version" -c "Add :CFBundleVersion string $version" -c 'Add :LSUIElement bool true' -c 'Add :LSMinimumSystemVersion string 12.0' "$app/Contents/Info.plist"
printf '%s\n' "version=$version" "commit=$(git rev-parse HEAD)" 'architecture=x86_64' 'developer_id_signed=false' 'notarized=false' > "$app/Contents/Resources/build-info.txt"
cp LICENSE THIRD_PARTY_NOTICES.md "$app/Contents/Resources/"
cp -R third_party_licenses "$app/Contents/Resources/"
# Local ad-hoc signature only; NOT an Apple Developer ID signature or notarization.
codesign --force --sign - "$app/Contents/MacOS/gitconverge"
codesign --force --sign - "$app"
codesign --verify --deep --strict "$app"
cp macos/install.sh macos/uninstall.sh macos/README.md "$root/"
cp config.macos.example.toml "$root/"
chmod 755 "$root/install.sh" "$root/uninstall.sh"
(cd dist && ditto -c -k --sequesterRsrc --keepParent "$name" "$name.zip" && shasum -a 256 "$name.zip" > "$name.zip.sha256")
printf 'Built %s\n' "dist/$name.zip"
