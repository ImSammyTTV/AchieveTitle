cask "achievetitle" do
  arch arm: "arm64", intel: "amd64"

  version "0.7.0"
  sha256 arm:   "a3ad42ccea34aad6eb2424b1b3b062d4ef838184f7c2fd22de892e051ca813ee",
         intel: "28638dde5b4148c8eb98383a181fefd302ed5fc1bc6b0d41da7850962135962b"

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
