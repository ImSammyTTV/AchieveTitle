package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAPIs stands in for Steam and Twitch and records every channel PATCH.
func fakeAPIs(t *testing.T, channel *Channel, patches *[]map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/GetPlayerSummaries/v2/"):
			w.Write([]byte(`{"response":{"players":[{"gameid":"1942280","gameextrainfo":"Brotato"}]}}`))
		case strings.HasSuffix(r.URL.Path, "/GetPlayerAchievements/v1/"):
			w.Write([]byte(`{"playerstats":{"success":true,"gameName":"Brotato","achievements":[
				{"apiname":"a","achieved":1,"unlocktime":10,"name":"Hoarder"},
				{"apiname":"b","achieved":0,"unlocktime":0,"name":"Speedrun"}]}}`))
		case strings.HasSuffix(r.URL.Path, "/GetGlobalAchievementPercentagesForApp/v2/"):
			w.Write([]byte(`{"achievementpercentages":{"achievements":[{"name":"a","percent":"12.5"},{"name":"b","percent":0.4}]}}`))
		case r.URL.Path == "/games":
			if r.URL.Query().Get("name") == "Brotato" {
				w.Write([]byte(`{"data":[{"id":"2222","name":"Brotato"}]}`))
			} else {
				w.Write([]byte(`{"data":[]}`))
			}
		case r.URL.Path == "/channels" && r.Method == "GET":
			json.NewEncoder(w).Encode(map[string]any{"data": []Channel{*channel}})
		case r.URL.Path == "/channels" && r.Method == "PATCH":
			var p map[string]any
			json.NewDecoder(r.Body).Decode(&p)
			*patches = append(*patches, p)
			if v, ok := p["title"].(string); ok {
				channel.Title = v
			}
			if v, ok := p["game_id"].(string); ok {
				channel.GameID = v
			}
			if v, ok := p["tags"].([]any); ok {
				channel.Tags = nil
				for _, x := range v {
					channel.Tags = append(channel.Tags, x.(string))
				}
			}
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
}

func TestWorkerUpdatesTitleCategoryTagsAndRestores(t *testing.T) {
	channel := &Channel{Title: "My normal title", GameID: "27471", GameName: "Minecraft", Tags: []string{"chillstream"}}
	var patches []map[string]any
	srv := fakeAPIs(t, channel, &patches)
	defer srv.Close()
	steamAPI, twitchAPI = srv.URL, srv.URL

	cfg := defaultConfig()
	cfg.SteamAPIKey, cfg.SteamID = "k", "76561198035066303"
	cfg.CustomTitle, cfg.Enabled = "Potato potata", true
	cfg.TwitchToken, cfg.TwitchUserID = "tok", "1"
	cfg.ManageTags, cfg.Tags = true, []string{"chill stream", "AchievementHunting"}
	w := newWorker(&Store{path: t.TempDir() + "/c.json", cfg: cfg})

	w.tick(context.Background())
	if len(patches) != 1 {
		t.Fatalf("want 1 update, got %v (status error: %s)", patches, w.Status().Error)
	}
	if channel.Title != "Potato potata [1/2 Achievements] 🏆 Hoarder" || channel.GameID != "2222" ||
		strings.Join(channel.Tags, ",") != "chillstream,AchievementHunting" {
		t.Fatalf("channel not updated as expected: %+v", channel)
	}
	if st := w.Status(); st.Category != "Brotato" || st.Error != "" {
		t.Fatalf("status: %+v", st)
	}

	w.tick(context.Background()) // nothing changed: Twitch must not be touched again
	if len(patches) != 1 {
		t.Fatalf("unchanged tick sent an update: %v", patches[1:])
	}

	w.Restore()
	if channel.Title != "My normal title" || channel.GameID != "27471" || strings.Join(channel.Tags, ",") != "chillstream" {
		t.Fatalf("not restored: %+v", channel)
	}
}
