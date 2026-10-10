package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var steamAPI = "https://api.steampowered.com"

var steamID64Re = regexp.MustCompile(`^\d{17}$`)

type Steam struct {
	http *http.Client
	// Cached per app: global unlock percentages rarely change.
	rarity   map[string]map[string]float64
	rarityAt map[string]time.Time
	icons    map[string]map[string][2]string // app -> achievement -> [icon, grey icon]
}

func newSteam() *Steam {
	return &Steam{
		http:     &http.Client{Timeout: 15 * time.Second},
		rarity:   map[string]map[string]float64{},
		rarityAt: map[string]time.Time{},
		icons:    map[string]map[string][2]string{},
	}
}

func (s *Steam) get(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", steamAPI+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 401, 403:
		return fmt.Errorf("steam rejected the request (HTTP %d): check your API key and that your profile + game details are public", resp.StatusCode)
	default:
		return fmt.Errorf("steam %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ResolveID turns a vanity name or profile URL into a SteamID64.
func (s *Steam) ResolveID(ctx context.Context, key, id string) (string, error) {
	id = regexp.MustCompile(`^https?://steamcommunity\.com/(id|profiles)/([^/]+)/?$`).ReplaceAllString(id, "$2")
	if steamID64Re.MatchString(id) {
		return id, nil
	}
	var r struct {
		Response struct {
			Success int    `json:"success"`
			SteamID string `json:"steamid"`
		} `json:"response"`
	}
	if err := s.get(ctx, "/ISteamUser/ResolveVanityURL/v1/", url.Values{"key": {key}, "vanityurl": {id}}, &r); err != nil {
		return "", err
	}
	if r.Response.Success != 1 {
		return "", fmt.Errorf("could not find Steam profile %q", id)
	}
	return r.Response.SteamID, nil
}

// CurrentGame returns the app ID and name of what the user is playing ("" if nothing).
func (s *Steam) CurrentGame(ctx context.Context, key, steamID string) (appID, name string, err error) {
	var r struct {
		Response struct {
			Players []struct {
				GameID   string `json:"gameid"`
				GameName string `json:"gameextrainfo"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := s.get(ctx, "/ISteamUser/GetPlayerSummaries/v2/", url.Values{"key": {key}, "steamids": {steamID}}, &r); err != nil {
		return "", "", err
	}
	if len(r.Response.Players) == 0 {
		return "", "", fmt.Errorf("steam profile not found")
	}
	p := r.Response.Players[0]
	return p.GameID, p.GameName, nil
}

type Achievement struct {
	APIName    string  `json:"api_name"`
	Name       string  `json:"name"`
	Desc       string  `json:"description"`
	Achieved   bool    `json:"achieved"`
	UnlockTime int64   `json:"unlock_time"`
	Percent    float64 `json:"global_percent"`
	Icon       string  `json:"icon,omitempty"`      // Steam's achievement art
	IconGray   string  `json:"icon_gray,omitempty"` // the greyed-out "locked" version
}

type Progress struct {
	AppID          string
	Game           string
	Achievements   []Achievement
	Unlocked       int
	Total          int
	Latest         *Achievement // most recently unlocked
	RarestLocked   *Achievement // lowest global % still locked
	RarestUnlocked *Achievement // lowest global % already unlocked
}

// Achievements returns progress, or (nil, nil) if the game has no achievements.
func (s *Steam) Achievements(ctx context.Context, key, steamID, appID string) (*Progress, error) {
	var r struct {
		PlayerStats struct {
			Success      bool   `json:"success"`
			Error        string `json:"error"`
			GameName     string `json:"gameName"`
			Achievements []struct {
				APIName    string `json:"apiname"`
				Achieved   int    `json:"achieved"`
				UnlockTime int64  `json:"unlocktime"`
				Name       string `json:"name"`
				Desc       string `json:"description"`
			} `json:"achievements"`
		} `json:"playerstats"`
	}
	q := url.Values{"key": {key}, "steamid": {steamID}, "appid": {appID}, "l": {"english"}}
	err := s.get(ctx, "/ISteamUserStats/GetPlayerAchievements/v1/", q, &r)
	if err != nil {
		return nil, err
	}
	if !r.PlayerStats.Success || len(r.PlayerStats.Achievements) == 0 {
		return nil, nil
	}
	rarity := s.globalRarity(ctx, appID)
	icons := s.achievementIcons(ctx, key, appID)
	p := &Progress{AppID: appID, Game: r.PlayerStats.GameName}
	for _, a := range r.PlayerStats.Achievements {
		p.Achievements = append(p.Achievements, Achievement{
			APIName: a.APIName, Name: a.Name, Desc: a.Desc,
			Achieved: a.Achieved == 1, UnlockTime: a.UnlockTime, Percent: rarity[a.APIName],
			Icon: icons[a.APIName][0], IconGray: icons[a.APIName][1],
		})
	}
	p.Total = len(p.Achievements)
	// Sort (newest unlock first) before taking pointers into the slice:
	// sorting moves elements, so earlier pointers would point at the wrong ones.
	sort.SliceStable(p.Achievements, func(i, j int) bool {
		return p.Achievements[i].UnlockTime > p.Achievements[j].UnlockTime
	})
	for i := range p.Achievements {
		a := &p.Achievements[i]
		if a.Achieved {
			p.Unlocked++
			if p.Latest == nil || a.UnlockTime > p.Latest.UnlockTime {
				p.Latest = a
			}
			if rarity != nil && a.Percent > 0 && (p.RarestUnlocked == nil || a.Percent < p.RarestUnlocked.Percent) {
				p.RarestUnlocked = a
			}
		} else if rarity != nil && (p.RarestLocked == nil || a.Percent < p.RarestLocked.Percent) {
			p.RarestLocked = a
		}
	}
	return p, nil
}

// achievementIcons fetches each achievement's art from the game's schema.
// Icons never change, so they're fetched once per game while the app runs.
func (s *Steam) achievementIcons(ctx context.Context, key, appID string) map[string][2]string {
	if m, ok := s.icons[appID]; ok {
		return m
	}
	var r struct {
		Game struct {
			Stats struct {
				Achievements []struct {
					Name     string `json:"name"`
					Icon     string `json:"icon"`
					IconGray string `json:"icongray"`
				} `json:"achievements"`
			} `json:"availableGameStats"`
		} `json:"game"`
	}
	if err := s.get(ctx, "/ISteamUserStats/GetSchemaForGame/v2/", url.Values{"key": {key}, "appid": {appID}}, &r); err != nil {
		return nil // art is optional; try again next check
	}
	m := map[string][2]string{}
	for _, a := range r.Game.Stats.Achievements {
		m[a.Name] = [2]string{a.Icon, a.IconGray}
	}
	s.icons[appID] = m
	return m
}

func (s *Steam) globalRarity(ctx context.Context, appID string) map[string]float64 {
	if m, ok := s.rarity[appID]; ok && time.Since(s.rarityAt[appID]) < 6*time.Hour {
		return m
	}
	var r struct {
		AP struct {
			Achievements []struct {
				Name    string          `json:"name"`
				Percent json.RawMessage `json:"percent"` // number or string depending on API version
			} `json:"achievements"`
		} `json:"achievementpercentages"`
	}
	if err := s.get(ctx, "/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v2/", url.Values{"gameid": {appID}}, &r); err != nil {
		return s.rarity[appID] // may be nil; rarity is optional
	}
	m := map[string]float64{}
	for _, a := range r.AP.Achievements {
		raw := string(a.Percent)
		if uq, err := strconv.Unquote(raw); err == nil {
			raw = uq
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			m[a.Name] = f
		}
	}
	s.rarity[appID], s.rarityAt[appID] = m, time.Now()
	return m
}
