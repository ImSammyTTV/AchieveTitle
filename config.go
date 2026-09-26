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

	TwitchClientID string `json:"twitch_client_id,omitempty"`
	TwitchToken    string `json:"twitch_token,omitempty"`
	TwitchUserID   string `json:"twitch_user_id,omitempty"`
	TwitchLogin    string `json:"twitch_login,omitempty"`
}

func defaultConfig() Config {
	return Config{
		CustomTitle:     "Chill stream",
		Template:        "{custom} [{unlocked}/{total} Achievements] 🏆 {latest}",
		FallbackTmpl:    "{custom}",
		IntervalSeconds: 60,
		RestoreOnExit:   true,
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
