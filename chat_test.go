package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestRenderChatDropsEmptyGroups(t *testing.T) {
	vars := map[string]string{"next": "Speedrunner", "next_rarity": ""}
	got := renderChat("Next hunt: {next} (only {next_rarity} of players have it)", vars)
	if got != "Next hunt: Speedrunner" {
		t.Fatalf("got %q", got)
	}
	vars["next_rarity"] = "0.8%"
	got = renderChat("Next hunt: {next} (only {next_rarity} of players have it)", vars)
	if got != "Next hunt: Speedrunner (only 0.8% of players have it)" {
		t.Fatalf("got %q", got)
	}
	if got := renderChat("plain (text) stays", nil); got != "plain (text) stays" {
		t.Fatalf("got %q", got)
	}
}

func TestProgressBar(t *testing.T) {
	if got := progressBar(45, 179, BarStyle{}); got != "▰▰▱▱▱▱▱▱▱▱" {
		t.Fatalf("default: got %q", got)
	}
	if got := progressBar(45, 179, BarStyle{Filled: "🟪", Empty: "⬛", Length: 8}); got != "🟪🟪⬛⬛⬛⬛⬛⬛" {
		t.Fatalf("emoji: got %q", got)
	}
	if got := progressBar(1, 2, BarStyle{Filled: "⭐️★", Empty: "", Length: 99}); got != strings.Repeat("⭐️", 10)+strings.Repeat("▱", 10) {
		t.Fatalf("cleaned: got %q", got)
	}
}

// fakeTwitchChat is an EventSub WebSocket plus the Helix endpoints chat uses.
type fakeTwitchChat struct {
	mu        sync.Mutex
	sent      []map[string]string
	subs      []map[string]any
	conn      *websocket.Conn
	connected chan struct{}
}

func (f *fakeTwitchChat) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws":
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			c.Write(r.Context(), websocket.MessageText, []byte(`{"metadata":{"message_type":"session_welcome"},"payload":{"session":{"id":"sess1","keepalive_timeout_seconds":10}}}`))
			f.mu.Lock()
			f.conn = c
			f.mu.Unlock()
			f.connected <- struct{}{}
			<-r.Context().Done()
		case "/eventsub/subscriptions":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			f.mu.Lock()
			f.subs = append(f.subs, b)
			f.mu.Unlock()
			w.WriteHeader(202)
			w.Write([]byte(`{"data":[]}`))
		case "/chat/messages":
			if r.Header.Get("Authorization") != "Bearer bot-token" {
				t.Errorf("message not sent as the bot: %s", r.Header.Get("Authorization"))
			}
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			f.mu.Lock()
			f.sent = append(f.sent, b)
			f.mu.Unlock()
			w.Write([]byte(`{"data":[{"is_sent":true}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	})
}

func (f *fakeTwitchChat) say(t *testing.T, chatterID, name, text string, badges ...string) {
	ev := map[string]any{"chatter_user_id": chatterID, "chatter_user_name": name, "message_id": "m-" + text,
		"message": map[string]string{"text": text}}
	var b []map[string]string
	for _, s := range badges {
		b = append(b, map[string]string{"set_id": s})
	}
	ev["badges"] = b
	msg, _ := json.Marshal(map[string]any{
		"metadata": map[string]string{"message_type": "notification", "subscription_type": "channel.chat.message"},
		"payload":  map[string]any{"event": ev},
	})
	f.mu.Lock()
	c := f.conn
	f.mu.Unlock()
	if err := c.Write(context.Background(), websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeTwitchChat) messages() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]string(nil), f.sent...)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestChatCommandsAndAnnouncements(t *testing.T) {
	f := &fakeTwitchChat{connected: make(chan struct{}, 1)}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	twitchAPI = srv.URL
	eventsubURL = "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	cfg := defaultConfig()
	cfg.TwitchToken, cfg.TwitchUserID, cfg.TwitchLogin = "chan-token", "100", "imsammy"
	cfg.BotToken, cfg.BotUserID, cfg.BotLogin = "bot-token", "200", "sammybot"
	cfg.Chat.Enabled = true
	store := &Store{path: t.TempDir() + "/c.json", cfg: cfg}
	w := newWorker(store)
	latest := Achievement{Name: "Hoarder", Achieved: true, UnlockTime: 100, Percent: 12.5}
	w.game, w.prog = "Brotato", &Progress{Unlocked: 45, Total: 179, Latest: &latest,
		Achievements: []Achievement{latest}}
	chat := newChat(store, w)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go chat.Run(ctx)
	<-f.connected
	waitFor(t, "connected status", func() bool { return chat.Status() == "connected as sammybot" })

	if len(f.subs) != 1 || f.subs[0]["condition"].(map[string]any)["user_id"] != "200" {
		t.Fatalf("bad subscription: %v", f.subs)
	}

	f.say(t, "300", "viewer1", "!achievements please")
	waitFor(t, "reply", func() bool { return len(f.messages()) == 1 })
	m := f.messages()[0]
	if m["message"] != "🏆 imsammy has 45/179 achievements in Brotato (25%). Latest: Hoarder" ||
		m["sender_id"] != "200" || m["broadcaster_id"] != "100" || m["reply_parent_message_id"] != "m-!achievements please" {
		t.Fatalf("reply: %v", m)
	}

	f.say(t, "301", "viewer2", "!achievements")          // on cooldown: ignored
	f.say(t, "200", "sammybot", "!progress")             // the bot itself: ignored
	f.say(t, "302", "mod", "!ACHIEVEMENTS", "moderator") // mods skip the cooldown
	waitFor(t, "mod reply", func() bool { return len(f.messages()) == 2 })
	time.Sleep(100 * time.Millisecond)
	if n := len(f.messages()); n != 2 {
		t.Fatalf("expected 2 replies, got %d: %v", n, f.messages())
	}

	chat.Announce([]Achievement{{Name: "Speedrunner", Achieved: true, Percent: 0.8}})
	msgs := f.messages()
	if last := msgs[len(msgs)-1]; last["message"] != "🎉 imsammy just unlocked Speedrunner (only 0.8% of players have it)!" || last["reply_parent_message_id"] != "" {
		t.Fatalf("announcement: %v", last)
	}

	// Switching chat off disconnects.
	store.Update(func(c *Config) { c.Chat.Enabled = false })
	chat.Reload()
	waitFor(t, "chat off", func() bool { return chat.Status() == "off" })
}

func TestChatAccountChoice(t *testing.T) {
	cfg := defaultConfig()
	cfg.TwitchToken, cfg.TwitchUserID, cfg.TwitchLogin = "chan", "100", "streamer"
	if _, err := account(cfg); err == nil || !strings.Contains(err.Error(), "bot account") {
		t.Fatalf("expected 'connect a bot' error, got %v", err)
	}
	cfg.Chat.ReplyAs = "channel"
	if _, err := account(cfg); err == nil || !strings.Contains(err.Error(), "reconnect") {
		t.Fatalf("old channel login without chat scopes should ask to reconnect, got %v", err)
	}
	cfg.TwitchScopes = strings.Fields(twitchScope)
	if acc, err := account(cfg); err != nil || acc.login != "streamer" {
		t.Fatalf("channel fallback: %v %v", acc, err)
	}
}

func TestTrackUnlocksAnnouncesOnlyNewOnes(t *testing.T) {
	w := newWorker(&Store{cfg: defaultConfig()})
	var got []string
	done := make(chan struct{}, 4)
	w.onUnlock = func(a []Achievement) {
		for _, x := range a {
			got = append(got, x.Name)
		}
		done <- struct{}{}
	}
	p := func(times ...int64) *Progress {
		p := &Progress{}
		for i, ts := range times {
			p.Achievements = append(p.Achievements, Achievement{Name: string(rune('A' + i)), Achieved: true, UnlockTime: ts})
		}
		return p
	}
	w.trackUnlocks("1", p(10, 5))         // first look at the game: quiet
	w.trackUnlocks("1", p(30, 20, 10, 5)) // two new ones
	<-done
	w.trackUnlocks("2", p(99))           // switched game: quiet
	if strings.Join(got, ",") != "B,A" { // oldest first
		t.Fatalf("announced %v", got)
	}
}
