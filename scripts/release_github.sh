#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="$root/dist"
version="${VERSION:-0.2.0}"
version="${version#v}"
tag="v${version}"
app="kuaima_cli"
pkg="./cmd/kuaima_cli"
flags=(-buildvcs=false -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=${version}")

command -v go >/dev/null || { echo "go is required" >&2; exit 1; }
command -v gh >/dev/null || { echo "GitHub CLI (gh) is required" >&2; exit 1; }
command -v zip >/dev/null || { echo "zip is required" >&2; exit 1; }

mkdir -p "$dist"
rm -f "$dist/${app}-${tag}-windows-amd64.zip" \
      "$dist/${app}-${tag}-macos-amd64.zip" \
      "$dist/${app}-${tag}-macos-arm64.zip"

build_archive() {
  local goos="$1" goarch="$2" binary="$3" archive="$4"
  echo "Building ${goos}/${goarch}..."
  (cd "$root" && GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
    go build "${flags[@]}" -o "$dist/$binary" "$pkg")
  (cd "$dist" && zip -j "$archive" "$binary" >/dev/null)
  rm -f "$dist/$binary"
}

build_archive windows amd64 "${app}.exe" "${app}-${tag}-windows-amd64.zip"

# DMG 构建需要 macOS runner，复用现有的 GitHub Actions workflow。
echo "Starting macOS DMG workflow..."
# Keep this compatible with the workflow currently installed on GitHub.  The
# workflow_dispatch input may not exist remotely until this change is pushed.
gh workflow run macos-dmg.yml --ref "$(git -C "$root" branch --show-current)"
run_id="$(gh run list --workflow=macos-dmg.yml --limit=1 --json databaseId --jq '.[0].databaseId')"
if [[ -z "$run_id" ]]; then
  echo "Unable to find the macOS DMG workflow run" >&2
  exit 1
fi
gh run watch "$run_id" --exit-status

mac_dist="$dist/github-macos-${tag}"
rm -rf "$mac_dist"
mkdir -p "$mac_dist"
gh run download "$run_id" --dir "$mac_dist"
mapfile -t dmg_files < <(find "$mac_dist" -type f -name '*.dmg')
if [[ "${#dmg_files[@]}" -eq 0 ]]; then
  echo "The macOS workflow did not produce a DMG" >&2
  exit 1
fi

assets=(
  "$dist/${app}-${tag}-windows-amd64.zip"
  "${dmg_files[@]}"
)

if gh release view "$tag" >/dev/null 2>&1; then
  echo "Uploading assets to existing GitHub release ${tag}..."
  gh release upload "$tag" "${assets[@]}" --clobber
else
  echo "Creating GitHub release ${tag}..."
  gh release create "$tag" "${assets[@]}" --title "$tag" --generate-notes
fi

echo "Published ${tag}."
