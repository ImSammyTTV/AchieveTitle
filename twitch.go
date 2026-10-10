package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// DefaultTwitchClientID is the public client ID of the official AchieveTitle
// Twitch app (a public client, so this is not a secret). Forks can override it
// with -ldflags "-X main.DefaultTwitchClientID=xxxx", and users can paste their
// own in the settings page.
var DefaultTwitchClientID = "e1n36uf3stt4tb0chevm8zbt6x0ix3"

// twitchScope is requested for the streamer's own channel: manage the stream
// info, plus chat so chat commands can reply from the channel when no bot
// account is connected.
const twitchScope = "channel:manage:broadcast user:read:chat user:write:chat"

// botScope is what the chat bot account needs: read chat and send messages as itself.
const botScope = "user:read:chat user:write:chat"

var twitchAPI = "https://api.twitch.tv/helix"

type Twitch struct {
	http   *http.Client
	mu     sync.Mutex
	byName map[string]Category // Steam game name -> category ("" ID = no match)
	byID   map[string]Category
}

func newTwitch() *Twitch {
	return &Twitch{http: &http.Client{Timeout: 15 * time.Second}, byName: map[string]Category{}, byID: map[string]Category{}}
}

// Category is a Twitch category (usually a game) with its box art.
type Category struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	BoxArt string `json:"box_art"`
}

type helixCategory struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	BoxArt string `json:"box_art_url"`
}

func (h helixCategory) category() Category {
	// Twitch returns a URL template ({width}x{height}) or a fixed small size; ask for 144x192.
	art := strings.NewReplacer("{width}", "144", "{height}", "192").Replace(h.BoxArt)
	art = boxArtSizeRe.ReplaceAllString(art, "-144x192.")
	return Category{ID: h.ID, Name: h.Name, BoxArt: art}
}

var boxArtSizeRe = regexp.MustCompile(`-\d+x\d+\.`)

var errTwitchAuth = fmt.Errorf("twitch login expired: reconnect Twitch in the settings page")

func twitchAuthURL(clientID, redirect, state, scope string) string {
	q := url.Values{
		"response_type": {"token"}, // implicit grant: no client secret needed in a desktop app
		"client_id":     {clientID},
		"redirect_uri":  {redirect},
		"scope":         {scope},
		"state":         {state},
		"force_verify":  {"true"},
	}
	return "https://id.twitch.tv/oauth2/authorize?" + q.Encode()
}

// Validate checks a token and returns the user it belongs to.
func (t *Twitch) Validate(ctx context.Context, token string) (userID, login string, scopes []string, err error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://id.twitch.tv/oauth2/validate", nil)
	req.Header.Set("Authorization", "OAuth "+token)
	resp, err := t.http.Do(req)
	if err != nil {
		return "", "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return "", "", nil, errTwitchAuth
	}
	var r struct {
		UserID string   `json:"user_id"`
		Login  string   `json:"login"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", nil, err
	}
	return r.UserID, r.Login, r.Scopes, nil
}

func (t *Twitch) do(ctx context.Context, cfg Config, method, path string, body any, out any) error {
	return t.doAs(ctx, cfg.TwitchToken, clientID(cfg), method, path, body, out)
}

// doAs calls the Helix API with a specific account's token (the channel or the bot).
func (t *Twitch) doAs(ctx context.Context, token, client, method, path string, body any, out any) error {
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
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Id", client)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return errTwitchAuth
	}
	if resp.StatusCode == 429 {
		return errTwitchTooFast
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
// name match and falling back to Twitch's search. Returns a zero Category if
// nothing clearly matches, so the stream never lands in the wrong game.
func (t *Twitch) FindCategory(ctx context.Context, cfg Config, game string) (Category, error) {
	key := strings.ToLower(game)
	t.mu.Lock()
	c, ok := t.byName[key]
	t.mu.Unlock()
	if ok {
		return c, nil
	}
	var r struct{ Data []helixCategory }
	if err := t.do(ctx, cfg, "GET", "/games?name="+url.QueryEscape(game), nil, &r); err != nil {
		return Category{}, err
	}
	if len(r.Data) == 0 {
		if err := t.do(ctx, cfg, "GET", "/search/categories?first=5&query="+url.QueryEscape(game), nil, &r); err != nil {
			return Category{}, err
		}
		want := normalizeName(game)
		var match []helixCategory
		for _, d := range r.Data {
			if normalizeName(d.Name) == want {
				match = append(match, d)
				break
			}
		}
		r.Data = match
	}
	if len(r.Data) > 0 {
		c = r.Data[0].category()
	}
	t.remember(key, c)
	return c, nil
}

// CategoryByID looks up a category's name and box art.
func (t *Twitch) CategoryByID(ctx context.Context, cfg Config, id string) (Category, error) {
	t.mu.Lock()
	c, ok := t.byID[id]
	t.mu.Unlock()
	if ok || id == "" {
		return c, nil
	}
	var r struct{ Data []helixCategory }
	if err := t.do(ctx, cfg, "GET", "/games?id="+url.QueryEscape(id), nil, &r); err != nil {
		return Category{}, err
	}
	if len(r.Data) > 0 {
		c = r.Data[0].category()
		t.remember("", c)
	}
	return c, nil
}

// SearchCategories powers the manual game picker.
func (t *Twitch) SearchCategories(ctx context.Context, cfg Config, q string) ([]Category, error) {
	var r struct{ Data []helixCategory }
	if err := t.do(ctx, cfg, "GET", "/search/categories?first=12&query="+url.QueryEscape(q), nil, &r); err != nil {
		return nil, err
	}
	out := []Category{}
	for _, d := range r.Data {
		c := d.category()
		t.remember("", c)
		out = append(out, c)
	}
	return out, nil
}

func (t *Twitch) remember(name string, c Category) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if name != "" {
		t.byName[name] = c
	}
	if c.ID != "" {
		t.byID[c.ID] = c
	}
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

// hasScopes reports whether granted includes every scope in the space-separated want.
func hasScopes(granted []string, want string) bool {
	for _, w := range strings.Fields(want) {
		found := false
		for _, g := range granted {
			found = found || g == w
		}
		if !found {
			return false
		}
	}
	return true
}

func clientID(cfg Config) string {
	if cfg.TwitchClientID != "" {
		return cfg.TwitchClientID
	}
	return DefaultTwitchClientID
}

// Avatar returns an account's Twitch profile picture URL.
func (t *Twitch) Avatar(ctx context.Context, token, client, userID string) (string, error) {
	var r struct {
		Data []struct {
			ProfileImageURL string `json:"profile_image_url"`
		}
	}
	if err := t.doAs(ctx, token, client, "GET", "/users?id="+url.QueryEscape(userID), nil, &r); err != nil {
		return "", err
	}
	if len(r.Data) == 0 {
		return "", fmt.Errorf("twitch user not found")
	}
	return r.Data[0].ProfileImageURL, nil
}
