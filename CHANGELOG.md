# Changelog

## v0.6.3
- 🖥️ The settings page now scales to fill your window: bigger and easier to read on large screens, still one screen with no scrolling
- ✨ Redesigned OBS overlay: a clear card with box art, progress and your latest unlock, plus a bigger "Achievement unlocked" popup
- The overlay now shows a small "waiting for a Steam game" card instead of nothing (or tick *Hide it when I'm not in a Steam game*)
- 🏆 New **Test popup** button in the OBS tab to preview the unlock animation

## v0.6.2
- The OBS tab's setup box shrinks to a single "✔ OBS is set up" line once it's done

## v0.6.1
- Fixed: *Set up OBS for me* couldn't find the scene collection in newer OBS versions ("Untitled.json.json")

## v0.6.0
- 🎥 **Set up OBS for me**: one click adds the AchieveTitle script and control panel to OBS (settings backed up first)
- ✨ Redesigned settings page: everything on one screen, with tabs, a live status card and recent unlocks
- 🎮 **Sign in with Steam** instead of typing your Steam ID, plus a guided API key step that checks your key instantly
- 💬 Chat commands: `!achievements`, `!last`, `!next`, `!rarest` and `!progress`
  - Each can be switched on or off, renamed and given your own reply
  - Replies come from **your own bot account** (or your channel account if you prefer)
  - Cooldown per command; you and your mods skip it
- 🎉 Optional chat announcement when you unlock an achievement
- New placeholders: `{rarest}`, `{rarest_rarity}`, `{bar}` (and `{recent}`, `{user}`, `{channel}` in chat)
- Fixed: the "latest" and "rarest missing" achievement could occasionally be the wrong one
- 🍎 One-line Mac installer that skips macOS's security prompts

**Using your channel account for chat replies?** Click *Connect Twitch* once more to give it chat permission.

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
