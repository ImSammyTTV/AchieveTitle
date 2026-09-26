#!/bin/sh
# Builds every download into dist/. Usage: packaging/build-release.sh v0.1.0
set -eu
TAG="$1"; VERSION="${TAG#v}"
LDFLAGS="-s -w -X main.version=$TAG"
rm -rf build && mkdir -p build dist   # dist/ may already hold the macOS zips

# Windows: app icon + version info, and no console window (-H windowsgui).
go-winres simply --icon assets/icon-1024.png --manifest gui --product-name AchieveTitle \
  --file-description AchieveTitle --product-version "$VERSION" --file-version "$VERSION" --arch amd64,arm64
trap 'rm -f rsrc_windows_*.syso' EXIT

# macOS is built separately on a Mac (packaging/build-mac.sh) for the menu bar icon.
for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64; do
  os=${target%/*}; arch=${target#*/}
  name="AchieveTitle-$TAG-$os-$arch"
  ext=""; flags="$LDFLAGS"
  [ "$os" = windows ] && ext=.exe && flags="$LDFLAGS -H windowsgui"
  mkdir -p "build/$name"
  GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$flags" -o "build/$name/AchieveTitle$ext" .
  cp README.md LICENSE "build/$name/"
  cp -r obs "build/$name/"
  (cd build && zip -qr "../dist/$name.zip" "$name")

  if [ "$os" = linux ]; then
    # Tarball with a fixed name so install.sh can always fetch the latest release.
    mkdir -p "build/linux-$arch"
    cp "build/$name/AchieveTitle" "build/linux-$arch/achievetitle"
    cp packaging/linux/* LICENSE "build/linux-$arch/"
    tar -czf "dist/achievetitle-linux-$arch.tar.gz" -C "build/linux-$arch" .
    for fmt in deb rpm apk archlinux; do
      # nfpm doesn't expand variables in file paths, so fill them in first.
      sed "s/\${ARCH}/$arch/g; s/\${VERSION}/$VERSION/g" packaging/nfpm.yaml > "build/nfpm-$arch.yaml"
      nfpm pkg -f "build/nfpm-$arch.yaml" -p "$fmt" -t dist/ >/dev/null
    done
  fi
done
(cd dist && rm -f SHA256SUMS.txt && sha256sum * > SHA256SUMS.txt)
ls -1 dist
