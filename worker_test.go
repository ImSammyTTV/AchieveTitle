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
		case strings.HasSuffix(r.URL.Path, "/GetSchemaForGame/v2/"):
			w.Write([]byte(`{"game":{"availableGameStats":{"achievements":[
				{"name":"a","icon":"https://cdn.example/a.jpg","icongray":"https://cdn.example/a_gray.jpg"},
				{"name":"b","icon":"https://cdn.example/b.jpg","icongray":"https://cdn.example/b_gray.jpg"}]}}}`))
		case strings.HasSuffix(r.URL.Path, "/GetGlobalAchievementPercentagesForApp/v2/"):
			w.Write([]byte(`{"achievementpercentages":{"achievements":[{"name":"a","percent":"12.5"},{"name":"b","percent":0.4}]}}`))
		case r.URL.Path == "/games":
			q := r.URL.Query()
			switch {
			case q.Get("name") == "Brotato" || q.Get("id") == "2222":
				w.Write([]byte(`{"data":[{"id":"2222","name":"Brotato","box_art_url":"https://static-cdn.jtvnw.net/ttv-boxart/2222-{width}x{height}.jpg"}]}`))
			case q.Get("id") == "27471":
				w.Write([]byte(`{"data":[{"id":"27471","name":"Minecraft","box_art_url":"https://static-cdn.jtvnw.net/ttv-boxart/27471_IGDB-52x72.jpg"}]}`))
			default:
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
	if st := w.Status(); st.Category != "Brotato" || st.Error != "" ||
		st.CategoryArt != "https://static-cdn.jtvnw.net/ttv-boxart/2222-144x192.jpg" {
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

func TestWorkerManualCategory(t *testing.T) {
	channel := &Channel{Title: "My normal title", GameID: "2222", GameName: "Brotato"}
	var patches []map[string]any
	srv := fakeAPIs(t, channel, &patches)
	defer srv.Close()
	steamAPI, twitchAPI = srv.URL, srv.URL

	cfg := defaultConfig()
	cfg.SteamAPIKey, cfg.SteamID, cfg.Enabled = "k", "76561198035066303", true
	cfg.TwitchToken, cfg.TwitchUserID = "tok", "1"
	// Steam says Brotato, but the streamer picked Minecraft themselves.
	cfg.CategoryMode = "manual"
	cfg.ManualCategory = &Category{ID: "27471", Name: "Minecraft"}
	w := newWorker(&Store{path: t.TempDir() + "/c.json", cfg: cfg})

	w.tick(context.Background())
	if channel.GameID != "27471" {
		t.Fatalf("manual category not applied: %+v (patches %v)", channel, patches)
	}
	st := w.Status()
	if st.Category != "Minecraft" || st.CategoryArt != "https://static-cdn.jtvnw.net/ttv-boxart/27471_IGDB-144x192.jpg" {
		t.Fatalf("status: category %q art %q", st.Category, st.CategoryArt)
	}
}

func TestChasing(t *testing.T) {
	channel := &Channel{Title: "t", GameID: "2222"}
	var patches []map[string]any
	srv := fakeAPIs(t, channel, &patches)
	defer srv.Close()
	steamAPI, twitchAPI = srv.URL, srv.URL

	cfg := defaultConfig()
	cfg.SteamAPIKey, cfg.SteamID, cfg.Enabled = "k", "76561198035066303", true
	cfg.TwitchToken, cfg.TwitchUserID = "tok", "1"
	cfg.Template = "{game} 🎯 {chasing} ({chasing_rarity})"
	cfg.Chasing = map[string]string{"1942280": "b"} // "Speedrun", still locked in the fake data
	store := &Store{path: t.TempDir() + "/c.json", cfg: cfg}
	w := newWorker(store)

	w.tick(context.Background())
	if st := w.Status(); st.Chasing == nil || st.Chasing.Name != "Speedrun" || st.Title != "Brotato 🎯 Speedrun (0.4%)" {
		t.Fatalf("chasing not shown: %+v title %q", st.Chasing, st.Title)
	}
	if st := w.Status(); st.Chasing.Icon != "https://cdn.example/b.jpg" || st.Latest == nil || st.Latest.Icon != "https://cdn.example/a.jpg" {
		t.Fatalf("achievement art missing: chasing %q latest %+v", st.Chasing.Icon, st.Latest)
	}
	if _, list := w.Locked(); len(list) != 1 || list[0].APIName != "b" {
		t.Fatalf("locked list: %+v", list)
	}

	// An achievement that's no longer locked (or doesn't exist) stops being chased.
	store.Update(func(c *Config) { c.Chasing["1942280"] = "a" }) // Hoarder is already unlocked
	w.tick(context.Background())
	if store.Get().Chasing["1942280"] != "" || w.Status().Chasing != nil {
		t.Fatalf("unlocked achievement still chased: %v", store.Get().Chasing)
	}
}
