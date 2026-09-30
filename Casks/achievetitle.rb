cask "achievetitle" do
  arch arm: "arm64", intel: "amd64"

  version "0.7.8"
  sha256 arm:   "0f518f6655570a36a3ee0f0117b2accfdfdb21dac0d9056a5c3b2276a2288a1f",
         intel: "a9bc93ad05fcc908ed5716e453e2f9348a8079179efa3e1248ff8a049f1c19fb"

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
