#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$script_dir/.." && pwd)"
dist="$root/dist"
app_name="kuaima_cli"
bundle_name="$app_name.app"
stage="$dist/macos-dmg"
bundle="$stage/$bundle_name"
contents="$bundle/Contents"
macos_dir="$contents/MacOS"
resources_dir="$contents/Resources"
engine_dir="$resources_dir/rembg-engine"
models_dir="$engine_dir/models"
version="${VERSION:-}"
runtime_version="${ONNXRUNTIME_VERSION:-1.28.0}"
runtime_archive="$dist/onnxruntime-osx-universal2-$runtime_version.tgz"
runtime_extract_dir="$dist/onnxruntime-osx-universal2-$runtime_version"
model_name="${REMBG_MODEL_NAME:-}"
model_url="${REMBG_MODEL_URL:-}"
usage_guide="$root/快马AI使用说明.txt"

if [[ -z "$version" && -f "$root/internal/app/version.go" ]]; then
  version="$(sed -nE 's/.*AppVersion[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' "$root/internal/app/version.go" | head -n 1)"
fi
if [[ -z "$version" ]]; then
  version="0.1.0"
fi
version="${version#v}"

amd64_bin="$dist/$app_name-darwin-amd64"
arm64_bin="$dist/$app_name-darwin-arm64"
universal_bin="$macos_dir/$app_name"
engine_amd64_bin="$dist/rembg-engine-darwin-amd64"
engine_arm64_bin="$dist/rembg-engine-darwin-arm64"
engine_universal_bin="$engine_dir/rembg-engine"
dmg_path="$dist/$app_name-macos-$version.dmg"

rm -rf "$stage" "$amd64_bin" "$arm64_bin" "$engine_amd64_bin" "$engine_arm64_bin" "$dmg_path"
mkdir -p "$macos_dir" "$resources_dir" "$models_dir"

GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-X kuaima_cli/internal/app.AppVersion=$version" -o "$amd64_bin" "$root/cmd/kuaima_cli"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-X kuaima_cli/internal/app.AppVersion=$version" -o "$arm64_bin" "$root/cmd/kuaima_cli"
GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$engine_amd64_bin" "$root/cmd/rembg_engine"
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$engine_arm64_bin" "$root/cmd/rembg_engine"

command -v lipo >/dev/null 2>&1 || {
  echo "lipo command not found." >&2
  exit 1
}
command -v sips >/dev/null 2>&1 || {
  echo "sips command not found." >&2
  exit 1
}
command -v iconutil >/dev/null 2>&1 || {
  echo "iconutil command not found." >&2
  exit 1
}

lipo -create "$amd64_bin" "$arm64_bin" -output "$universal_bin"
lipo -create "$engine_amd64_bin" "$engine_arm64_bin" -output "$engine_universal_bin"
chmod +x "$universal_bin" "$engine_universal_bin"

if [[ ! -f "$runtime_archive" ]]; then
  curl --fail --location --retry 3 \
    "https://github.com/microsoft/onnxruntime/releases/download/v$runtime_version/onnxruntime-osx-universal2-$runtime_version.tgz" \
    --output "$runtime_archive"
fi
if [[ ! -f "$runtime_extract_dir/lib/libonnxruntime.dylib" ]]; then
  rm -rf "$runtime_extract_dir"
  tar -xzf "$runtime_archive" -C "$dist"
fi
cp -L "$runtime_extract_dir/lib/libonnxruntime.dylib" "$engine_dir/libonnxruntime.dylib"

if [[ -n "$model_url" || -n "$model_name" ]]; then
  if [[ -z "$model_url" || -z "$model_name" ]]; then
    echo "REMBG_MODEL_URL and REMBG_MODEL_NAME must be set together." >&2
    exit 1
  fi
  model_cache="$dist/rembg-models/$model_name"
  mkdir -p "$(dirname "$model_cache")"
  if [[ ! -f "$model_cache" ]]; then
    curl --fail --location --retry 3 "$model_url" --output "$model_cache"
  fi
  cp "$model_cache" "$models_dir/$model_name"
else
  echo "No rembg model configured; packaging an empty models directory."
fi

iconset_dir="$stage/kuaima_cli.iconset"
app_icon="$resources_dir/AppIcon.icns"
icon_source="$root/internal/app/webui/logo.png"
normalized_icon="$(mktemp "$dist/logo.normalized.XXXXXX.png")"
normalize_helper="$(mktemp "$dist/logo.normalize.XXXXXX.go")"
cleanup_icon_tmp() {
  rm -f "$normalized_icon" "$normalize_helper"
}
trap cleanup_icon_tmp EXIT

cat > "$normalize_helper" <<'EOF'
package main

import (
	"fmt"
	"image/png"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: normalize_png <input> <output>")
		os.Exit(2)
	}

	in, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer in.Close()

	img, err := png.Decode(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	out, err := os.Create(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer out.Close()

	if err := png.Encode(out, img); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
EOF

if [[ ! -f "$icon_source" ]]; then
  echo "Missing icon source: $icon_source" >&2
  exit 1
fi

go run "$normalize_helper" "$icon_source" "$normalized_icon"
rm -f "$normalize_helper"
icon_source="$normalized_icon"

rm -rf "$iconset_dir"
mkdir -p "$iconset_dir"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$icon_source" --out "$iconset_dir/icon_${size}x${size}.png" >/dev/null
  double_size=$((size * 2))
  sips -z "$double_size" "$double_size" "$icon_source" --out "$iconset_dir/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset_dir" -o "$app_icon"
rm -rf "$iconset_dir"

ln -s /Applications "$stage/Applications"
cp "$usage_guide" "$stage/"

cat > "$contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleExecutable</key>
  <string>$app_name</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>CFBundleIconName</key>
  <string>AppIcon</string>
  <key>CFBundleIdentifier</key>
  <string>com.kuaima.cli</string>
  <key>CFBundleName</key>
  <string>$app_name</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$version</string>
  <key>CFBundleVersion</key>
  <string>$version</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>LSUIElement</key>
  <true/>
</dict>
</plist>
EOF

hdiutil create \
  -volname "$app_name" \
  -srcfolder "$stage" \
  -ov \
  -format UDZO \
  "$dmg_path"

echo "$dmg_path"
