#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -ne 1 ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

version="$1"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z]+)*$ ]]; then
  echo "unsupported release version: $version" >&2
  exit 2
fi
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
package_name="GitConverge-${version}-linux-amd64"
package_dir="${root}/dist/${package_name}"
archive="${root}/dist/${package_name}.tar.gz"

cd "$root"
go test ./...
go vet ./...
GOOS=windows GOARCH=amd64 pwsh -NoProfile -File ./scripts/Test-ThirdPartyLicenses.ps1

rm -rf "$package_dir" "$archive" "${archive}.sha256"
mkdir -p "$package_dir/third_party_licenses"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath \
  -ldflags "-s -w -X main.version=${version}" \
  -o "$package_dir/gitconverge" \
  ./cmd/gitconverge
[[ "$("$package_dir/gitconverge" version)" == "GitConverge ${version}" ]]
cp config.linux.example.toml LICENSE THIRD_PARTY_NOTICES.md "$package_dir/"
cp -R third_party_licenses/. "$package_dir/third_party_licenses/"
tar -C "${root}/dist" -czf "$archive" "$package_name"
(
  cd "${root}/dist"
  sha256sum "${package_name}.tar.gz" > "${package_name}.tar.gz.sha256"
)
