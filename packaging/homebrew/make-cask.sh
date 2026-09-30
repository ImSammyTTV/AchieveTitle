#!/bin/sh
# Writes the Homebrew cask for a release to stdout.
#   packaging/homebrew/make-cask.sh v0.6.9 dist/SHA256SUMS.txt > Casks/achievetitle.rb
set -eu
TAG="$1"; SUMS="$2"
sum() { grep " AchieveTitle-$TAG-darwin-$1.zip\$" "$SUMS" | awk '{print $1}'; }
ARM="$(sum arm64)"; INTEL="$(sum amd64)"
[ -n "$ARM" ] && [ -n "$INTEL" ] || { echo "No macOS downloads for $TAG in $SUMS" >&2; exit 1; }
cat <<CASK
cask "achievetitle" do
  arch arm: "arm64", intel: "amd64"

  version "${TAG#v}"
  sha256 arm:   "$ARM",
         intel: "$INTEL"

  url "https://github.com/ImSammyTTV/AchieveTitle/releases/download/v#{version}/AchieveTitle-v#{version}-darwin-#{arch}.zip"
  name "AchieveTitle"
  desc "Live Steam achievement progress in your Twitch stream title"
  homepage "https://github.com/ImSammyTTV/AchieveTitle"

  livecheck do
    url :url
    strategy :github_latest
  end

  # AchieveTitle updates itself from inside the app.
  auto_updates true
  depends_on macos: ">= :big_sur"

  app "AchieveTitle.app"

  # The app isn't signed with an Apple developer account, so clear the
  # download quarantine to stop macOS refusing to open it.
  postflight do
    system_command "/usr/bin/xattr",
                   args: ["-dr", "com.apple.quarantine", "#{appdir}/AchieveTitle.app"]
  end

  uninstall quit: "io.github.imsammyttv.AchieveTitle"

  zap trash: "~/Library/Application Support/AchieveTitle"
end
CASK
