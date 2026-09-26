#!/bin/sh
# Builds every download into dist/. Usage: packaging/build-release.sh v0.1.0
set -eu
TAG="$1"; VERSION="${TAG#v}"
LDFLAGS="-s -w -X main.version=$TAG"
rm -rf build dist && mkdir -p build dist

for target in windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
  os=${target%/*}; arch=${target#*/}
  name="AchieveTitle-$TAG-$os-$arch"
  ext=""; [ "$os" = windows ] && ext=.exe
  mkdir -p "build/$name"
  GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "build/$name/AchieveTitle$ext" .
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
(cd dist && sha256sum * > SHA256SUMS.txt)
ls -1 dist
