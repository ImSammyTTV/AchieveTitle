package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// steamOpenID is Steam's "Sign in through Steam" endpoint. It only tells us
// the user's SteamID; no password or key ever reaches AchieveTitle.
var steamOpenID = "https://steamcommunity.com/openid/login"

var steamClaimedIDRe = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/(\d{17})$`)

// steamLoginURL sends the user to Steam's sign-in page, which returns to
// returnTo afterwards.
func steamLoginURL(realm, returnTo string) string {
	q := url.Values{
		"openid.ns":         {"http://specs.openid.net/auth/2.0"},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {realm},
		"openid.identity":   {"http://specs.openid.net/auth/2.0/identifier_select"},
		"openid.claimed_id": {"http://specs.openid.net/auth/2.0/identifier_select"},
	}
	return steamOpenID + "?" + q.Encode()
}

// VerifySteamLogin checks the sign-in response with Steam itself (so it can't
// be faked) and returns the SteamID64 it vouches for.
func (s *Steam) VerifySteamLogin(ctx context.Context, q url.Values, wantReturnTo string) (string, error) {
	if q.Get("openid.mode") != "id_res" {
		return "", fmt.Errorf("Steam sign-in was cancelled")
	}
	if q.Get("openid.return_to") != wantReturnTo {
		return "", fmt.Errorf("Steam sign-in expired, please try again")
	}
	m := steamClaimedIDRe.FindStringSubmatch(q.Get("openid.claimed_id"))
	if m == nil {
		return "", fmt.Errorf("Steam didn't return a Steam account")
	}

	check := url.Values{}
	for k, v := range q {
		if strings.HasPrefix(k, "openid.") {
			check[k] = v
		}
	}
	check.Set("openid.mode", "check_authentication")
	req, err := http.NewRequestWithContext(ctx, "POST", steamOpenID, strings.NewReader(check.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking Steam sign-in: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if !strings.Contains(string(body), "is_valid:true") {
		return "", fmt.Errorf("Steam couldn't confirm the sign-in, please try again")
	}
	return m[1], nil
}

// SteamProfile is shown in the settings page so people can see which account is linked.
type SteamProfile struct {
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Public  bool   `json:"public"`
	Profile string `json:"profile"`
}

// Profile looks up the account's name and avatar. It doubles as the API key
// check: Steam rejects the request if the key is wrong.
func (s *Steam) Profile(ctx context.Context, key, steamID string) (SteamProfile, error) {
	var r struct {
		Response struct {
			Players []struct {
				Name       string `json:"personaname"`
				Avatar     string `json:"avatarmedium"`
				URL        string `json:"profileurl"`
				Visibility int    `json:"communityvisibilitystate"` // 3 = public
			} `json:"players"`
		} `json:"response"`
	}
	if err := s.get(ctx, "/ISteamUser/GetPlayerSummaries/v2/", url.Values{"key": {key}, "steamids": {steamID}}, &r); err != nil {
		return SteamProfile{}, err
	}
	if len(r.Response.Players) == 0 {
		return SteamProfile{}, fmt.Errorf("Steam account not found")
	}
	p := r.Response.Players[0]
	return SteamProfile{Name: p.Name, Avatar: p.Avatar, Public: p.Visibility == 3, Profile: p.URL}, nil
}
