package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
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
	Chat         string        `json:"chat"`
	Chasing      *Achievement  `json:"chasing"`      // the achievement being chased, if any
	OverlayTest  int64         `json:"overlay_test"` // bumped by the "Test popup" button
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

	// For chat: the latest progress, and what has already been announced.
	game       string
	prog       *Progress
	seenApp    string
	seenUnlock int64
	onUnlock   func([]Achievement)
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

	w.trackUnlocks(appID, prog)
	// Stop chasing an achievement once it's unlocked (the unlock itself is celebrated as usual).
	if prog != nil && cfg.Chasing[appID] != "" && chasingIn(cfg, prog) == nil {
		w.store.Update(func(c *Config) { delete(c.Chasing, appID) })
		cfg = w.store.Get()
	}

	tmpl := cfg.FallbackTmpl
	if prog != nil {
		tmpl = cfg.Template
		if game == "" {
			game = prog.Game
		}
	}
	// {game} falls back to the category the streamer picked, but the status
	// only ever names a game Steam actually reports.
	steamGame := game
	if game == "" && cfg.CategoryMode == "manual" && cfg.ManualCategory != nil {
		game = cfg.ManualCategory.Name
	}
	title := renderTitle(tmpl, buildVars(cfg, game, prog))

	w.mu.Lock()
	w.game, w.prog = game, prog
	w.status.Chasing = chasingIn(cfg, prog)
	w.status.Game, w.status.AppID, w.status.Title = steamGame, appID, title
	w.status.Unlocked, w.status.Total, w.status.Latest, w.status.Next, w.status.Recent = 0, 0, nil, nil, nil
	if prog != nil {
		w.status.Unlocked, w.status.Total = prog.Unlocked, prog.Total
		w.status.Latest, w.status.Next = prog.Latest, prog.RarestLocked
		for _, a := range prog.Achievements {
			if a.Achieved && len(w.status.Recent) < 6 {
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

// trackUnlocks announces achievements unlocked since the last check. It stays
// quiet on the first check of a game, so switching games doesn't spam chat.
func (w *Worker) trackUnlocks(appID string, p *Progress) {
	if p == nil {
		w.seenApp, w.seenUnlock = appID, 0
		return
	}
	var newest int64
	var fresh []Achievement
	for _, a := range p.Achievements { // newest first
		if !a.Achieved {
			continue
		}
		newest = max(newest, a.UnlockTime)
		if w.seenApp == appID && a.UnlockTime > w.seenUnlock && len(fresh) < 3 {
			fresh = append(fresh, a)
		}
	}
	w.seenApp, w.seenUnlock = appID, max(newest, w.seenUnlock)
	if len(fresh) > 0 && w.onUnlock != nil {
		// Oldest first, so chat reads in the order they were unlocked.
		slices.Reverse(fresh)
		go w.onUnlock(fresh)
	}
}

// ChatVars returns the placeholders for chat replies, from the latest check.
func (w *Worker) ChatVars() map[string]string {
	w.mu.RLock()
	game, prog := w.game, w.prog
	w.mu.RUnlock()
	return buildVars(w.store.Get(), game, prog)
}

// Locked lists the current game's still-locked achievements, rarest first,
// for the "Chasing" picker.
func (w *Worker) Locked() (appID string, list []Achievement) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.prog == nil {
		return "", nil
	}
	for _, a := range w.prog.Achievements {
		if !a.Achieved {
			list = append(list, a)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		pi, pj := list[i].Percent, list[j].Percent
		if pi == 0 || pj == 0 { // unknown rarity goes last
			return pj == 0 && pi != 0
		}
		return pi < pj
	})
	return w.prog.AppID, list
}
