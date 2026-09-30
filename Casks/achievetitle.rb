cask "achievetitle" do
  arch arm: "arm64", intel: "amd64"

  version "0.7.5"
  sha256 arm:   "f58d2819b15f9f9bc22d0eaf6954a77daa84036f7b125ea4d3e0df42746ab30c",
         intel: "18e5931beab71f83838d6c563ac77bf3b8ff38ce806f8dcc3d46c699394b1edf"

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
