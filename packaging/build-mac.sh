#!/bin/sh
# Builds AchieveTitle.app for macOS (Apple Silicon + Intel) into dist/.
# Must run on a Mac: the menu bar icon needs cgo. Usage: packaging/build-mac.sh v0.4.0
set -eu
TAG="$1"; VERSION="${TAG#v}"
rm -rf build-mac && mkdir -p build-mac dist

# App icon (.icns) from the 1024px PNG.
ICONSET=build-mac/AchieveTitle.iconset; mkdir -p "$ICONSET"
for s in 16 32 128 256 512; do
  sips -z $s $s assets/icon-1024.png --out "$ICONSET/icon_${s}x${s}.png" >/dev/null
  sips -z $((s*2)) $((s*2)) assets/icon-1024.png --out "$ICONSET/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o build-mac/AchieveTitle.icns

for arch in arm64 amd64; do
  name="AchieveTitle-$TAG-darwin-$arch"
  app="build-mac/$name/AchieveTitle.app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  CGO_ENABLED=1 GOOS=darwin GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$TAG" -o "$app/Contents/MacOS/AchieveTitle" .
  cp build-mac/AchieveTitle.icns "$app/Contents/Resources/"
  sed "s/__VERSION__/$VERSION/g" packaging/macos/Info.plist > "$app/Contents/Info.plist"
  codesign --force --deep -s - "$app"   # ad-hoc signature (required on Apple Silicon)
  cp README.md LICENSE "build-mac/$name/"
  cp -r obs "build-mac/$name/"
  (cd build-mac && ditto -c -k --sequesterRsrc --keepParent "$name" "../dist/$name.zip")
done
ls -1 dist
