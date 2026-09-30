cask "achievetitle" do
  arch arm: "arm64", intel: "amd64"

  version "0.7.6"
  sha256 arm:   "305c736bff4a67c4c7aaf54c3372c10bd36b770408ca26b034c2c5bcbc0c2d6a",
         intel: "d33c6f9d6ac039366b1fb0ae024bb4a878648bcec397bc0afed1a34c7f12d4d1"

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
