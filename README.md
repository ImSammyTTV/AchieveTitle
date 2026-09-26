# AchieveTitle

Show live **Steam achievement progress** in your **Twitch stream title** — automatically.

> `Chill stream [38/50 Achievements] 🏆 Dragon Slayer`

Runs on **Windows, macOS and Linux**. One small download, nothing else to install.

## Features

- Updates your Twitch title as you unlock achievements (only when something changes)
- Detects the Steam game you're playing; uses a fallback title for games without achievements
- Sets your Twitch **category** to that game, and can manage your stream **tags**
- Title templates with placeholders, auto-shortened to fit Twitch's 140-character limit
- Optional **OBS overlay**: progress bar + "Achievement unlocked" popup with rarity
- Restores your original title when you stop or close the app
- Everything stays on your PC — settings page runs locally at `http://localhost:7878`

## Setup

1. Download the zip for your system from [Releases](../../releases) and unzip it.
2. Run **AchieveTitle** (on macOS: right-click → Open the first time). Your browser opens the settings page.
3. **Connect Twitch.**
4. Paste your **Steam Web API key** (get one at <https://steamcommunity.com/dev/apikey>) and your **Steam ID**
   (SteamID64, custom URL name, or profile link).
5. Make your Steam profile and **Game details** public: Steam → Profile → Edit Profile → Privacy Settings.
6. Write your title template, tick **Update my Twitch title automatically**, and Save.

Keep the app running while you stream.

### Linux

Works on every distro, including Steam Deck:

```
curl -fsSL https://raw.githubusercontent.com/ImSammyTTV/achievetitle/main/install.sh | sh
```

Or grab a package from [Releases](../../releases): `.deb` (Ubuntu, Debian, Mint, Pop!_OS),
`.rpm` (Fedora, openSUSE), `.pkg.tar.zst` (Arch, Manjaro, EndeavourOS, CachyOS) or `.apk` (Alpine).
Start at login (optional): `systemctl --user enable --now achievetitle`.
Uninstall the script version with `… | sh -s -- --uninstall`.

### OBS

- **Control panel inside OBS:** View → Docks → Custom Browser Docks, name `AchieveTitle`,
  URL `http://localhost:7878/dock`. Shows your title and progress with an on/off switch.
- **OBS script:** Tools → Scripts → `+` → choose `obs/achievetitle.lua` (included in every download).
  It starts AchieveTitle with OBS and only updates your title while you're live.
- If you also use OBS's own *Stream Information* panel, remember it overwrites the title when you press *Update* there.

### Placeholders

| Placeholder | Example |
|---|---|
| `{custom}` | your custom text |
| `{game}` | Elden Ring |
| `{unlocked}` / `{total}` / `{remaining}` | 38 / 50 / 12 |
| `{percent}` | 76% |
| `{latest}` / `{latest_rarity}` | last unlocked achievement / 4.2% |
| `{next}` / `{next_rarity}` | rarest achievement you're still missing / 0.8% |

### OBS overlay

Add a **Browser Source** with URL `http://localhost:7878/overlay` (600 × 160).

## Command-line options

```
AchieveTitle -port 7878 -no-browser
```

Settings are saved in your user config folder (`%AppData%\AchieveTitle`, `~/Library/Application Support/AchieveTitle`, or `~/.config/AchieveTitle`).

## Building from source

Requires Go 1.22+.

```
go build .
```

Builds use the official AchieveTitle Twitch app. To use your own, create a Public Twitch app at
<https://dev.twitch.tv/console> with OAuth redirect URL `http://localhost:7878/auth/callback`, then build with
`-ldflags "-X main.DefaultTwitchClientID=YOUR_CLIENT_ID"` or paste the ID into the settings page.

### Releasing (maintainers)

Push a tag such as `v0.1.0`. Packaging files for the AUR and Flathub are in `packaging/`.
GitHub Actions builds zips for Windows, macOS and Linux (x64 + ARM) and attaches them to a release.

## License

MIT
