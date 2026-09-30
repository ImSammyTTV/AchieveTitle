#!/bin/sh
# AchieveTitle installer for macOS (Apple Silicon and Intel).
#   curl -fsSL https://raw.githubusercontent.com/ImSammyTTV/AchieveTitle/main/install-mac.sh | sh
# Uninstall:
#   curl -fsSL https://raw.githubusercontent.com/ImSammyTTV/AchieveTitle/main/install-mac.sh | sh -s -- --uninstall
#
# Downloads the latest release from GitHub, checks it against the release's
# SHA256SUMS.txt and installs AchieveTitle.app into Applications.
set -eu

REPO="ImSammyTTV/AchieveTitle"
APP="AchieveTitle.app"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "This installer is for macOS. On Linux use install.sh instead." >&2
  exit 1
fi

# /Applications if we can write to it, otherwise the user's own Applications folder.
DEST="/Applications"
[ -w "$DEST" ] || DEST="$HOME/Applications"

if [ "${1:-}" = "--uninstall" ]; then
  osascript -e 'quit app "AchieveTitle"' >/dev/null 2>&1 || true
  rm -rf "/Applications/$APP" "$HOME/Applications/$APP"
  echo "AchieveTitle removed. Your settings are kept in ~/Library/Application Support/AchieveTitle."
  exit 0
fi

case "$(uname -m)" in
  arm64) ARCH=arm64 ;;
  x86_64) ARCH=amd64 ;;
  *) echo "Sorry, $(uname -m) Macs aren't supported." >&2; exit 1 ;;
esac

echo "Finding the latest AchieveTitle release…"
RELEASE="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")"
url_for() { printf '%s\n' "$RELEASE" | grep -o "\"browser_download_url\": *\"[^\"]*$1\"" | sed 's/.*"\(https[^"]*\)"/\1/' | head -n 1; }
ZIP_URL="$(url_for "darwin-$ARCH.zip")"
SUMS_URL="$(url_for "SHA256SUMS.txt")"
if [ -z "$ZIP_URL" ] || [ -z "$SUMS_URL" ]; then
  echo "Couldn't find a macOS download in the latest release." >&2
  exit 1
fi
ZIP_NAME="${ZIP_URL##*/}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
echo "Downloading $ZIP_NAME…"
curl -fsSL "$ZIP_URL" -o "$TMP/$ZIP_NAME"
curl -fsSL "$SUMS_URL" -o "$TMP/SHA256SUMS.txt"

WANT="$(grep " $ZIP_NAME\$" "$TMP/SHA256SUMS.txt" | awk '{print $1}')"
GOT="$(shasum -a 256 "$TMP/$ZIP_NAME" | awk '{print $1}')"
if [ -z "$WANT" ] || [ "$WANT" != "$GOT" ]; then
  echo "The download doesn't match the release checksum. Nothing was installed." >&2
  exit 1
fi

ditto -x -k "$TMP/$ZIP_NAME" "$TMP/unpacked"
SRC="$(find "$TMP/unpacked" -maxdepth 2 -name "$APP" -type d | head -n 1)"
if [ -z "$SRC" ]; then
  echo "The download doesn't contain $APP." >&2
  exit 1
fi

osascript -e 'quit app "AchieveTitle"' >/dev/null 2>&1 || true
mkdir -p "$DEST"
rm -rf "$DEST/$APP"
ditto "$SRC" "$DEST/$APP"
# Downloads made by curl aren't quarantined; clear the flag anyway in case
# the app was copied from somewhere that set it.
xattr -dr com.apple.quarantine "$DEST/$APP" 2>/dev/null || true

echo
echo "✔ AchieveTitle installed in $DEST. Starting it now…"
echo "  Look for the 🏆 in your menu bar. Your settings page will open in your browser."
open "$DEST/$APP"
