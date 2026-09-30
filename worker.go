package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Status is what the settings page and the OBS overlay display.
type Status struct {
	Running      bool          `json:"running"`
	Game         string        `json:"game"`
	AppID        string        `json:"app_id"`
	Unlocked     int           `json:"unlocked"`
	Total        int           `json:"total"`
	Latest       *Achievement  `json:"latest"`
	Next         *Achievement  `json:"next"`
	Recent       []Achievement `json:"recent"`
	Title        string        `json:"title"`
	OriginalTitl string        `json:"original_title"`
	Category     string        `json:"category"`
	CategoryArt  string        `json:"category_art"`
	CategoryMode string        `json:"category_mode"`
	Tags         []string      `json:"tags"`
	LastCheck    time.Time     `json:"last_check"`
	LastUpdate   time.Time     `json:"last_update"`
	Error        string        `json:"error"`
	TwitchLogin  string        `json:"twitch_login"`
}

type Worker struct {
	store  *Store
	steam  *Steam
	twitch *Twitch
	kick   chan struct{}

	runMu    sync.Mutex // serialises tick and Restore
	mu       sync.RWMutex
	status   Status
	steamID  string   // resolved SteamID64
	resolved string   // the input it was resolved from
	original *Channel // channel info before we first changed it
}

func newWorker(s *Store) *Worker {
	w := &Worker{store: s, steam: newSteam(), twitch: newTwitch(), kick: make(chan struct{}, 1)}
	w.original = s.Get().OriginalChannel // survives restarts
	if w.original != nil {
		w.status.OriginalTitl = w.original.Title
	}
	return w
}

func (w *Worker) Status() Status {
	w.mu.RLock()
	defer w.mu.RUnlock()
	st := w.status
	st.TwitchLogin = w.store.Get().TwitchLogin
	st.Running = w.store.Get().Enabled
	st.CategoryMode = w.store.Get().CategoryMode
	return st
}

// Kick triggers an immediate check (e.g. after saving settings).
func (w *Worker) Kick() {
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) {
	for {
		w.tick(ctx)
		wait := time.Duration(w.store.Get().IntervalSeconds) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case <-time.After(wait):
		}
	}
}

func (w *Worker) setErr(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.LastCheck = time.Now()
	if err != nil {
		w.status.Error = err.Error()
		log.Println("error:", err)
	} else {
		w.status.Error = ""
	}
}

func (w *Worker) tick(ctx context.Context) {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	cfg := w.store.Get()
	if cfg.SteamAPIKey == "" || cfg.SteamID == "" {
		w.setErr(errors.New("add your Steam API key and Steam ID in settings"))
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	if w.resolved != cfg.SteamID {
		id, err := w.steam.ResolveID(ctx, cfg.SteamAPIKey, cfg.SteamID)
		if err != nil {
			w.setErr(err)
			return
		}
		w.steamID, w.resolved = id, cfg.SteamID
	}

	appID, game, err := w.steam.CurrentGame(ctx, cfg.SteamAPIKey, w.steamID)
	if err != nil {
		w.setErr(err)
		return
	}
	var prog *Progress
	if appID != "" {
		if prog, err = w.steam.Achievements(ctx, cfg.SteamAPIKey, w.steamID, appID); err != nil {
			w.setErr(err)
			return
		}
	}

	tmpl := cfg.FallbackTmpl
	if prog != nil {
		tmpl = cfg.Template
		if game == "" {
			game = prog.Game
		}
	}
	if game == "" && cfg.CategoryMode == "manual" && cfg.ManualCategory != nil {
		game = cfg.ManualCategory.Name
	}
	title := renderTitle(tmpl, buildVars(cfg, game, prog))

	w.mu.Lock()
	w.status.Game, w.status.AppID, w.status.Title = game, appID, title
	w.status.Unlocked, w.status.Total, w.status.Latest, w.status.Next, w.status.Recent = 0, 0, nil, nil, nil
	if prog != nil {
		w.status.Unlocked, w.status.Total = prog.Unlocked, prog.Total
		w.status.Latest, w.status.Next = prog.Latest, prog.RarestLocked
		for _, a := range prog.Achievements {
			if a.Achieved && len(w.status.Recent) < 5 {
				w.status.Recent = append(w.status.Recent, a)
			}
		}
	}
	w.mu.Unlock()

	if !cfg.Enabled || cfg.TwitchToken == "" || title == "" {
		w.setErr(nil)
		return
	}
	ch, err := w.twitch.GetChannel(ctx, cfg)
	if err != nil {
		w.setErr(err)
		return
	}
	if w.original == nil {
		orig := ch
		w.original = &orig
		w.store.Update(func(c *Config) { c.OriginalChannel = &orig })
		w.mu.Lock()
		w.status.OriginalTitl = ch.Title
		w.mu.Unlock()
	}

	patch := map[string]any{}
	if ch.Title != title {
		patch["title"] = title
	}
	var catErr error
	if cfg.SetCategory {
		var target Category
		switch {
		case cfg.CategoryMode == "manual" && cfg.ManualCategory != nil:
			target = *cfg.ManualCategory
		case cfg.CategoryMode != "manual" && game != "":
			c, err := w.twitch.FindCategory(ctx, cfg, game)
			switch {
			case err != nil:
				catErr = err
			case c.ID == "":
				catErr = fmt.Errorf("no Twitch category found for %q, category left unchanged (you can pick one yourself in settings)", game)
			}
			target = c
		}
		if target.ID != "" && target.ID != ch.GameID {
			patch["game_id"] = target.ID
			ch.GameID, ch.GameName = target.ID, target.Name
		}
	}
	if cfg.ManageTags {
		if tags := CleanTags(cfg.Tags); !sameTags(tags, ch.Tags) {
			patch["tags"] = tags
			ch.Tags = tags
		}
	}
	if len(patch) > 0 {
		if err := w.twitch.UpdateChannel(ctx, cfg, patch); err != nil {
			w.setErr(err)
			return
		}
		log.Printf("twitch channel updated: %v", patch)
		w.mu.Lock()
		w.status.LastUpdate = time.Now()
		w.mu.Unlock()
	}
	art := ""
	if c, err := w.twitch.CategoryByID(ctx, cfg, ch.GameID); err == nil {
		art = c.BoxArt
	}
	w.mu.Lock()
	w.status.Category, w.status.CategoryArt, w.status.Tags = ch.GameName, art, ch.Tags
	w.mu.Unlock()
	w.setErr(catErr)
}

func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Restore puts the stream title back to what it was before AchieveTitle
// started, plus the category and tags if AchieveTitle manages those.
func (w *Worker) Restore() {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	cfg := w.store.Get()
	if w.original == nil || cfg.TwitchToken == "" {
		return
	}
	patch := map[string]any{"title": w.original.Title}
	if cfg.SetCategory && w.original.GameID != "" {
		patch["game_id"] = w.original.GameID
	}
	if cfg.ManageTags {
		patch["tags"] = w.original.Tags
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.twitch.UpdateChannel(ctx, cfg, patch); err != nil {
		log.Println("could not restore channel:", err)
		return
	}
	log.Printf("title restored: %s", w.original.Title)
	w.original = nil
	w.store.Update(func(c *Config) { c.OriginalChannel = nil })
}
