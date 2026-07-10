#!/usr/bin/env bash
set -euo pipefail

skill_name="${1:-kuaima-gpt-image-2}"

case "$skill_name" in
  ""|*[!A-Za-z0-9._-]*)
    echo "Invalid skill name: $skill_name" >&2
    exit 1
    ;;
esac

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
dist="$root/dist"
skill_source="$root/skill/$skill_name"
version="${VERSION:-}"

if [[ -z "$version" ]]; then
  version="0.1.0"
fi
version="${version#v}"

cli_output="$dist/kuaima_cli.exe"
cli_darwin_arm64="$dist/kuaima_cli-darwin-arm64"
cli_darwin_amd64="$dist/kuaima_cli-darwin-amd64"
cli_linux_arm64="$dist/kuaima_cli-linux-arm64"
cli_linux_amd64="$dist/kuaima_cli-linux-amd64"

if [[ ! -d "$skill_source" ]]; then
  echo "Skill source directory not found: $skill_source" >&2
  exit 1
fi

mkdir -p "$dist"

go build -buildvcs=false -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=$version" -o "$cli_output" "$root/cmd/kuaima_cli"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=$version" -o "$cli_darwin_arm64" "$root/cmd/kuaima_cli"
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=$version" -o "$cli_darwin_amd64" "$root/cmd/kuaima_cli"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=$version" -o "$cli_linux_arm64" "$root/cmd/kuaima_cli"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags "-s -w -X kuaima_cli/internal/app.AppVersion=$version" -o "$cli_linux_amd64" "$root/cmd/kuaima_cli"

command -v zip >/dev/null 2>&1 || {
  echo "zip command not found. Please install zip and retry." >&2
  exit 1
}

package_skill_with_bins() {
  local platform_name="$1"
  local stage_name="$2"
  local skill_markdown="$3"
  shift 3

  local stage="$dist/$stage_name"
  local zip_path="$dist/$stage_name.zip"
  local script_dir_out="$stage/scripts"
  local bin_dir="$stage/bin"
  local cwd
  local source_path
  local dest_name

  rm -rf "$stage" "$zip_path"
  mkdir -p "$script_dir_out" "$bin_dir"
  cp -R "$skill_source/agents" "$stage/"
  cp "$skill_source/$skill_markdown" "$stage/SKILL.md"

  cp "$skill_source/$1" "$script_dir_out/"
  cp "$skill_source/$2" "$script_dir_out/"
  shift 2

  while (($#)); do
    source_path="${1%%:*}"
    dest_name="${1#*:}"
    cp "$source_path" "$bin_dir/$dest_name"
    shift
  done

  cwd="$PWD"
  cd "$dist"
  zip -qr "$zip_path" "$stage_name"
  cd "$cwd"

  echo "Skill package [$platform_name]: $stage"
  echo "Skill archive [$platform_name]: $zip_path"
}

package_skill_with_bins "Windows" "$skill_name-windows" "SKILL.windows.md" \
  "scripts/kuaima-image.ps1" \
  "scripts/kuaima-recharge.ps1" \
  "$cli_output:kuaima_cli.exe"

package_skill_with_bins "macOS" "$skill_name-darwin" "SKILL.darwin.md" \
  "scripts/kuaima-image.sh" \
  "scripts/kuaima-recharge.sh" \
  "$cli_darwin_arm64:kuaima_cli-darwin-arm64" \
  "$cli_darwin_amd64:kuaima_cli-darwin-amd64"

package_skill_with_bins "Linux" "$skill_name-linux" "SKILL.linux.md" \
  "scripts/kuaima-image.sh" \
  "scripts/kuaima-recharge.sh" \
  "$cli_linux_arm64:kuaima_cli-linux-arm64" \
  "$cli_linux_amd64:kuaima_cli-linux-amd64"
