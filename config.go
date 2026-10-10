package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Config is persisted as JSON in the user's config directory.
type Config struct {
	SteamAPIKey     string `json:"steam_api_key"`
	SteamID         string `json:"steam_id"` // SteamID64 or vanity name
	CustomTitle     string `json:"custom_title"`
	Template        string `json:"template"`
	FallbackTmpl    string `json:"fallback_template"`
	IntervalSeconds int    `json:"interval_seconds"`
	Enabled         bool   `json:"enabled"`
	RestoreOnExit   bool   `json:"restore_on_exit"`

	SetCategory bool `json:"set_category"` // let AchieveTitle set the Twitch category
	// "auto": match the Steam game being played; "manual": always use ManualCategory.
	CategoryMode   string    `json:"category_mode"`
	ManualCategory *Category `json:"manual_category,omitempty"`
	ManageTags     bool      `json:"manage_tags"`
	Tags           []string  `json:"tags"`

	CheckUpdates bool `json:"check_updates"`

	Bar     BarStyle      `json:"bar"`     // how {bar} looks
	Overlay OverlayConfig `json:"overlay"` // unlock animation and sound

	// Chasing is the achievement the streamer is going for, per Steam app ID.
	Chasing map[string]string `json:"chasing,omitempty"`

	// Chat commands are answered by a separate bot account the streamer connects.
	Chat         ChatConfig `json:"chat"`
	TwitchScopes []string   `json:"twitch_scopes,omitempty"` // granted to the channel login
	BotToken     string     `json:"bot_token,omitempty"`
	BotUserID    string     `json:"bot_user_id,omitempty"`
	BotLogin     string     `json:"bot_login,omitempty"`
	BotAvatar    string     `json:"bot_avatar,omitempty"`

	// Channel info from before AchieveTitle changed it, kept on disk so it can
	// still be restored after a crash or an update restart.
	OriginalChannel *Channel `json:"original_channel,omitempty"`

	TwitchClientID string `json:"twitch_client_id,omitempty"`
	TwitchToken    string `json:"twitch_token,omitempty"`
	TwitchUserID   string `json:"twitch_user_id,omitempty"`
	TwitchLogin    string `json:"twitch_login,omitempty"`
	TwitchAvatar   string `json:"twitch_avatar,omitempty"`
}

// ChatCommand is one chat command, e.g. "!achievements".
type ChatCommand struct {
	ID      string `json:"id"` // stable key; the trigger can be renamed
	Enabled bool   `json:"enabled"`
	Trigger string `json:"trigger"`
	Reply   string `json:"reply"`
}

type ChatConfig struct {
	Enabled bool `json:"enabled"`
	// ReplyAs is "bot" (the connected bot account) or "channel" (the streamer's own account).
	ReplyAs          string        `json:"reply_as"`
	CooldownSeconds  int           `json:"cooldown_seconds"`
	Commands         []ChatCommand `json:"commands"`
	AnnounceUnlocks  bool          `json:"announce_unlocks"`
	AnnounceTemplate string        `json:"announce_template"`
	NoGameReply      string        `json:"no_game_reply"`
}

var defaultChatCommands = []ChatCommand{
	{ID: "achievements", Enabled: true, Trigger: "!achievements", Reply: "🏆 {channel} has {unlocked}/{total} achievements in {game} ({percent}). Latest: {latest}"},
	{ID: "last", Enabled: true, Trigger: "!last", Reply: "Latest unlocks: {recent}"},
	{ID: "next", Enabled: true, Trigger: "!next", Reply: "Next hunt: {next} (only {next_rarity} of players have it)"},
	{ID: "rarest", Enabled: true, Trigger: "!rarest", Reply: "Rarest unlock so far: {rarest} (only {rarest_rarity} of players have it)"},
	{ID: "progress", Enabled: true, Trigger: "!progress", Reply: "{game}: {bar} {percent} ({unlocked}/{total})"},
	{ID: "chasing", Enabled: true, Trigger: "!chasing", Reply: "🎯 Currently chasing {chasing} (only {chasing_rarity} of players have it). {chasing_desc}"},
}

func defaultChat() ChatConfig {
	c := ChatConfig{
		ReplyAs:          "bot",
		CooldownSeconds:  30,
		AnnounceUnlocks:  true,
		AnnounceTemplate: "🎉 {channel} just unlocked {latest} (only {latest_rarity} of players have it)!",
		NoGameReply:      "{channel} isn't playing a Steam game with achievements right now.",
	}
	c.fillDefaults()
	return c
}

// fillDefaults adds commands that are missing from older settings files,
// so new commands appear after an update without touching existing ones.
func (c *ChatConfig) fillDefaults() {
	have := map[string]bool{}
	for _, cmd := range c.Commands {
		have[cmd.ID] = true
	}
	for _, d := range defaultChatCommands {
		if !have[d.ID] {
			c.Commands = append(c.Commands, d)
		}
	}
	if c.CooldownSeconds < 5 {
		c.CooldownSeconds = 30
	}
}

func defaultConfig() Config {
	return Config{
		Chat:            defaultChat(),
		CustomTitle:     "Chill stream",
		Template:        "{custom} [{unlocked}/{total} Achievements] 🏆 {latest}",
		FallbackTmpl:    "{custom}",
		IntervalSeconds: 60,
		RestoreOnExit:   true,
		SetCategory:     true,
		CategoryMode:    "auto",
		CheckUpdates:    true,
		Bar:             defaultBarStyle(),
		Overlay:         defaultOverlay(),
	}
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "AchieveTitle", "config.json"), nil
}

func loadStore() (*Store, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	s := &Store{path: p, cfg: defaultConfig()}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.cfg); err != nil {
		return nil, err
	}
	s.cfg.Chat.fillDefaults()
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	if s.cfg.IntervalSeconds < 30 {
		s.cfg.IntervalSeconds = 30
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s.cfg, "", "  ")
	// 0600: file contains the Steam API key and Twitch token.
	return os.WriteFile(s.path, b, 0o600)
}
