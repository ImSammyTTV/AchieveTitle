# AchieveTitle

Show live **Steam achievement progress** in your **Twitch stream title** — automatically.

> `Chill stream [38/50 Achievements] Last: Dragon Slayer`

Runs on **Windows, macOS and Linux**. One small download, nothing else to install.

## Features

- Updates your Twitch title as you unlock achievements (only when something changes)
- Detects the Steam game you're playing; uses a fallback title for games without achievements
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
go build -ldflags "-X main.DefaultTwitchClientID=YOUR_CLIENT_ID" .
```

Without a built-in client ID, the settings page asks for one. Create a Twitch app at
<https://dev.twitch.tv/console> with OAuth redirect URL `http://localhost:7878/auth/callback`.

### Releasing (maintainers)

Add the Twitch app's Client ID as repository secret `TWITCH_CLIENT_ID`, then push a tag such as `v0.1.0`.
GitHub Actions builds zips for Windows, macOS and Linux (x64 + ARM) and attaches them to a release.

## License

MIT
