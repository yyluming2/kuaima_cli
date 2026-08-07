#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist="$root/dist"
version="${VERSION:-0.2.0}"
version="${version#v}"
tag="v${version}"

command -v gh >/dev/null || { echo "GitHub CLI (gh) is required" >&2; exit 1; }

mkdir -p "$dist"
branch="$(git -C "$root" branch --show-current)"
if [[ -z "$branch" ]]; then
  echo "release_github must run from a branch, not a detached HEAD" >&2
  exit 1
fi

dispatch_workflow() {
  local workflow="$1" previous_id new_id
  previous_id="$(gh run list --workflow="$workflow" --branch "$branch" --event workflow_dispatch --limit=1 --json databaseId --jq '.[0].databaseId // empty')"
  gh workflow run "$workflow" --ref "$branch" -f "version=$version" >&2
  for _ in {1..30}; do
    new_id="$(gh run list --workflow="$workflow" --branch "$branch" --event workflow_dispatch --limit=1 --json databaseId --jq '.[0].databaseId // empty')"
    if [[ -n "$new_id" && "$new_id" != "$previous_id" ]]; then
      printf '%s\n' "$new_id"
      return
    fi
    sleep 2
  done
  echo "Unable to find the newly dispatched $workflow run" >&2
  return 1
}

echo "Starting complete Windows and macOS package workflows..."
windows_run_id="$(dispatch_workflow offline-windows.yml)"
macos_run_id="$(dispatch_workflow macos-dmg.yml)"
gh run watch "$windows_run_id" --exit-status
gh run watch "$macos_run_id" --exit-status

github_dist="$dist/github-release-${tag}"
rm -rf "$github_dist"
mkdir -p "$github_dist/windows" "$github_dist/macos"
gh run download "$windows_run_id" --dir "$github_dist/windows"
gh run download "$macos_run_id" --dir "$github_dist/macos"
mapfile -t windows_files < <(find "$github_dist/windows" -type f -name '*-windows-amd64-offline.zip')
mapfile -t dmg_files < <(find "$github_dist/macos" -type f -name '*.dmg')
if [[ "${#windows_files[@]}" -eq 0 ]]; then
  echo "The Windows workflow did not produce an offline ZIP" >&2
  exit 1
fi
if [[ "${#dmg_files[@]}" -eq 0 ]]; then
  echo "The macOS workflow did not produce a DMG" >&2
  exit 1
fi

assets=(
  "${windows_files[@]}"
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
