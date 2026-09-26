package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// DefaultTwitchClientID is the public client ID of the official AchieveTitle
// Twitch app (a public client, so this is not a secret). Forks can override it
// with -ldflags "-X main.DefaultTwitchClientID=xxxx", and users can paste their
// own in the settings page.
var DefaultTwitchClientID = "e1n36uf3stt4tb0chevm8zbt6x0ix3"

const twitchScope = "channel:manage:broadcast"

var twitchAPI = "https://api.twitch.tv/helix"

type Twitch struct {
	http       *http.Client
	categories map[string][2]string // game name -> [id, name]; used only from the worker goroutine
}

func newTwitch() *Twitch {
	return &Twitch{http: &http.Client{Timeout: 15 * time.Second}, categories: map[string][2]string{}}
}

var errTwitchAuth = fmt.Errorf("twitch login expired: reconnect Twitch in the settings page")

func twitchAuthURL(clientID, redirect, state string) string {
	q := url.Values{
		"response_type": {"token"}, // implicit grant: no client secret needed in a desktop app
		"client_id":     {clientID},
		"redirect_uri":  {redirect},
		"scope":         {twitchScope},
		"state":         {state},
		"force_verify":  {"true"},
	}
	return "https://id.twitch.tv/oauth2/authorize?" + q.Encode()
}

// Validate checks a token and returns the user it belongs to.
func (t *Twitch) Validate(ctx context.Context, token string) (userID, login string, err error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://id.twitch.tv/oauth2/validate", nil)
	req.Header.Set("Authorization", "OAuth "+token)
	resp, err := t.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return "", "", errTwitchAuth
	}
	var r struct {
		UserID string   `json:"user_id"`
		Login  string   `json:"login"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	return r.UserID, r.Login, nil
}

func (t *Twitch) do(ctx context.Context, cfg Config, method, path string, body any, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, twitchAPI+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.TwitchToken)
	req.Header.Set("Client-Id", clientID(cfg))
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return errTwitchAuth
	}
	if resp.StatusCode >= 300 {
		var e struct{ Message string }
		json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("twitch %s: HTTP %d %s", path, resp.StatusCode, e.Message)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Channel is the part of a Twitch channel AchieveTitle manages.
type Channel struct {
	Title    string   `json:"title"`
	GameID   string   `json:"game_id"`
	GameName string   `json:"game_name"`
	Tags     []string `json:"tags"`
}

func (t *Twitch) GetChannel(ctx context.Context, cfg Config) (Channel, error) {
	var r struct{ Data []Channel }
	if err := t.do(ctx, cfg, "GET", "/channels?broadcaster_id="+url.QueryEscape(cfg.TwitchUserID), nil, &r); err != nil {
		return Channel{}, err
	}
	if len(r.Data) == 0 {
		return Channel{}, fmt.Errorf("twitch channel not found")
	}
	if r.Data[0].Tags == nil {
		r.Data[0].Tags = []string{}
	}
	return r.Data[0], nil
}

// UpdateChannel changes only the fields present in the patch
// (title, game_id, tags).
func (t *Twitch) UpdateChannel(ctx context.Context, cfg Config, patch map[string]any) error {
	return t.do(ctx, cfg, "PATCH", "/channels?broadcaster_id="+url.QueryEscape(cfg.TwitchUserID), patch, nil)
}

// FindCategory maps a Steam game name to a Twitch category, preferring an exact
// name match and falling back to Twitch's search. Returns "" if nothing fits.
func (t *Twitch) FindCategory(ctx context.Context, cfg Config, game string) (id, name string, err error) {
	if c, ok := t.categories[strings.ToLower(game)]; ok {
		return c[0], c[1], nil
	}
	var r struct {
		Data []struct{ ID, Name string }
	}
	if err := t.do(ctx, cfg, "GET", "/games?name="+url.QueryEscape(game), nil, &r); err != nil {
		return "", "", err
	}
	if len(r.Data) == 0 {
		if err := t.do(ctx, cfg, "GET", "/search/categories?first=5&query="+url.QueryEscape(game), nil, &r); err != nil {
			return "", "", err
		}
		// Only accept a search hit that is clearly the same game, so we never
		// put the stream in a wrong category.
		want := normalizeName(game)
		for _, d := range r.Data {
			if normalizeName(d.Name) == want {
				r.Data = []struct{ ID, Name string }{d}
				break
			}
		}
		if len(r.Data) != 1 || normalizeName(r.Data[0].Name) != want {
			r.Data = nil
		}
	}
	if len(r.Data) > 0 {
		id, name = r.Data[0].ID, r.Data[0].Name
	}
	t.categories[strings.ToLower(game)] = [2]string{id, name}
	return id, name, nil
}

// normalizeName ignores case, punctuation and ™/® marks: "Brotato™" == "brotato".
func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CleanTags applies Twitch's tag rules: at most 10 tags, each up to 25
// letters/digits with no spaces or symbols. Duplicates are dropped.
func CleanTags(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		var b strings.Builder
		for _, r := range t {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		tag := b.String()
		if r := []rune(tag); len(r) > 25 {
			tag = string(r[:25])
		}
		if tag == "" || seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		out = append(out, tag)
		if len(out) == 10 {
			break
		}
	}
	return out
}

func clientID(cfg Config) string {
	if cfg.TwitchClientID != "" {
		return cfg.TwitchClientID
	}
	return DefaultTwitchClientID
}
