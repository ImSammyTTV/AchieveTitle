package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DefaultTwitchClientID is the public client ID of the official AchieveTitle
// Twitch app (a public client, so this is not a secret). Forks can override it
// with -ldflags "-X main.DefaultTwitchClientID=xxxx", and users can paste their
// own in the settings page.
var DefaultTwitchClientID = "e1n36uf3stt4tb0chevm8zbt6x0ix3"

const twitchScope = "channel:manage:broadcast"

type Twitch struct{ http *http.Client }

func newTwitch() *Twitch { return &Twitch{http: &http.Client{Timeout: 15 * time.Second}} }

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
	req, err := http.NewRequestWithContext(ctx, method, "https://api.twitch.tv/helix"+path, rd)
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

func (t *Twitch) GetTitle(ctx context.Context, cfg Config) (string, error) {
	var r struct {
		Data []struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := t.do(ctx, cfg, "GET", "/channels?broadcaster_id="+url.QueryEscape(cfg.TwitchUserID), nil, &r); err != nil {
		return "", err
	}
	if len(r.Data) == 0 {
		return "", fmt.Errorf("twitch channel not found")
	}
	return r.Data[0].Title, nil
}

func (t *Twitch) SetTitle(ctx context.Context, cfg Config, title string) error {
	return t.do(ctx, cfg, "PATCH", "/channels?broadcaster_id="+url.QueryEscape(cfg.TwitchUserID),
		map[string]string{"title": title}, nil)
}

func clientID(cfg Config) string {
	if cfg.TwitchClientID != "" {
		return cfg.TwitchClientID
	}
	return DefaultTwitchClientID
}
