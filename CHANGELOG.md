# Changelog

## v0.5.0
- Status box and OBS panel show the Twitch category's box art
- Choose your category yourself: search Twitch categories (with box art) and pick one,
  or keep matching the Steam game automatically
- `{game}` uses your chosen game when Steam isn't showing one

## v0.4.0
- Runs as a proper app: no command window on Windows, a real AchieveTitle.app on macOS
- Tray / menu bar icon with Open settings, auto-update on/off, current title and Quit
- App icon on Windows and macOS
- Built-in updates: AchieveTitle tells you when a new version is out and installs it
  with one click (checksum-verified, keeps your settings, restarts by itself)
- Original title/category/tags are remembered across restarts and crashes
- Log file next to your settings (achievetitle.log) for troubleshooting

## v0.3.0
- Sets your Twitch category to the Steam game you're playing (on by default)
- Optional: manage your stream tags from AchieveTitle
- Restore also puts back your original category and tags
- Title length is now counted the way Twitch counts it (emoji count as 2)
- New default title: `[45/179 Achievements] 🏆 Hoarder`
- Settings page: current title and its length shown in separate boxes

## v0.2.0
- Linux: one-line installer for any distro (incl. Steam Deck), .deb, .rpm, Arch and Alpine packages,
  app-menu entry with icon, optional systemd user service
- OBS: control panel dock (`/dock`) and an OBS script that starts AchieveTitle with OBS
  and only updates the title while you're live
- Quit button in the settings page
- Launching a second copy opens the existing settings page
- SHA256SUMS.txt for verifying downloads
- AUR and Flatpak packaging files in `packaging/`

## v0.1.0
- First release
