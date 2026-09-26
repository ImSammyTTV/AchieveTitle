#!/bin/sh
# AchieveTitle installer for any Linux distro (including Steam Deck). No root needed.
#   curl -fsSL https://raw.githubusercontent.com/ImSammyTTV/achievetitle/main/install.sh | sh
# Uninstall:
#   curl -fsSL https://raw.githubusercontent.com/ImSammyTTV/achievetitle/main/install.sh | sh -s -- --uninstall
set -eu

REPO="ImSammyTTV/achievetitle"
BIN_DIR="$HOME/.local/bin"
DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}"
CFG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}"
APP_ID="io.github.imsammyttv.AchieveTitle"

uninstall() {
  systemctl --user disable --now achievetitle 2>/dev/null || true
  rm -f "$BIN_DIR/achievetitle" \
        "$DATA_DIR/applications/$APP_ID.desktop" \
        "$DATA_DIR/icons/hicolor/scalable/apps/$APP_ID.svg" \
        "$CFG_DIR/systemd/user/achievetitle.service"
  echo "AchieveTitle removed. Your settings are kept in $CFG_DIR/AchieveTitle (delete that folder to remove them)."
}

if [ "${1:-}" = "--uninstall" ]; then uninstall; exit 0; fi

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Sorry, $(uname -m) is not supported yet." >&2; exit 1 ;;
esac

if command -v curl >/dev/null 2>&1; then fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then fetch() { wget -qO "$2" "$1"; }
else echo "Please install curl or wget first." >&2; exit 1; fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
echo "Downloading AchieveTitle for linux-$ARCH…"
fetch "https://github.com/$REPO/releases/latest/download/achievetitle-linux-$ARCH.tar.gz" "$TMP/a.tgz"
tar -xzf "$TMP/a.tgz" -C "$TMP"

mkdir -p "$BIN_DIR" "$DATA_DIR/applications" "$DATA_DIR/icons/hicolor/scalable/apps" "$CFG_DIR/systemd/user"
install -m 755 "$TMP/achievetitle" "$BIN_DIR/achievetitle"
install -m 644 "$TMP/achievetitle.svg" "$DATA_DIR/icons/hicolor/scalable/apps/$APP_ID.svg"
# Absolute path, so the app menu works even if ~/.local/bin isn't on PATH.
sed "s|^Exec=.*|Exec=$BIN_DIR/achievetitle|" "$TMP/$APP_ID.desktop" > "$DATA_DIR/applications/$APP_ID.desktop"
sed "s|/usr/bin/achievetitle|$BIN_DIR/achievetitle|" "$TMP/achievetitle.service" > "$CFG_DIR/systemd/user/achievetitle.service"
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$DATA_DIR/applications" 2>/dev/null || true

echo
echo "✔ AchieveTitle installed. Open it from your app menu, or run: $BIN_DIR/achievetitle"
echo "  Start automatically at login (optional): systemctl --user enable --now achievetitle"
