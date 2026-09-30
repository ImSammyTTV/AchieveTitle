cask "achievetitle" do
  arch arm: "arm64", intel: "amd64"

  version "0.7.1"
  sha256 arm:   "00af9074cc7aa58cf129c8206e2186c88060f4379ebf21968b216be3bc199198",
         intel: "14b2ad2df73ab47840f1845622d562bc4ca5a739bb3b299bc146086347ef3fdd"

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
