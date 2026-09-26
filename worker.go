package main

import (
	"context"
	"errors"
	"log"
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
	steamID  string // resolved SteamID64
	resolved string // the input it was resolved from
	original string // title before we first changed it
}

func newWorker(s *Store) *Worker {
	return &Worker{store: s, steam: newSteam(), twitch: newTwitch(), kick: make(chan struct{}, 1)}
}

func (w *Worker) Status() Status {
	w.mu.RLock()
	defer w.mu.RUnlock()
	st := w.status
	st.TwitchLogin = w.store.Get().TwitchLogin
	st.Running = w.store.Get().Enabled
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
	current, err := w.twitch.GetTitle(ctx, cfg)
	if err != nil {
		w.setErr(err)
		return
	}
	if w.original == "" {
		w.original = current
		w.mu.Lock()
		w.status.OriginalTitl = current
		w.mu.Unlock()
	}
	if current != title {
		if err := w.twitch.SetTitle(ctx, cfg, title); err != nil {
			w.setErr(err)
			return
		}
		log.Printf("title updated: %s", title)
		w.mu.Lock()
		w.status.LastUpdate = time.Now()
		w.mu.Unlock()
	}
	w.setErr(nil)
}

// Restore puts the stream title back to what it was before AchieveTitle started.
func (w *Worker) Restore() {
	w.runMu.Lock()
	defer w.runMu.Unlock()
	cfg := w.store.Get()
	if w.original == "" || cfg.TwitchToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.twitch.SetTitle(ctx, cfg, w.original); err != nil {
		log.Println("could not restore title:", err)
		return
	}
	log.Printf("title restored: %s", w.original)
	w.original = ""
}
