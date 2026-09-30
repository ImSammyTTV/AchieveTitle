// AchieveTitle shows live Steam achievement progress in your Twitch stream title.
package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

var version = "dev"

func main() {
	port := flag.Int("port", 7878, "port for the local settings page")
	noBrowser := flag.Bool("no-browser", false, "don't open the settings page on start")
	noTray := flag.Bool("no-tray", false, "don't show a tray / menu bar icon")
	restarted := flag.Bool("restarted", false, "internal: started by an update")
	flag.Parse()
	setupLogging()

	a, ok := startApp(*port, *restarted)
	if !ok {
		// Most likely already running (e.g. launched twice from the app menu):
		// just show the existing settings page.
		if !*noBrowser {
			openBrowser(fmt.Sprintf("http://localhost:%d", *port))
		}
		return
	}
	if !*noBrowser && !*restarted {
		openBrowser(a.srv.base)
	}
	if !*noTray && trayAvailable() {
		runTray(a)
	} else {
		a.wait()
	}
}

// setupLogging writes the log to a file next to the settings, because the
// Windows and macOS builds have no console window to show it in.
func setupLogging() {
	p, err := configPath()
	if err != nil {
		return
	}
	logPath := filepath.Join(filepath.Dir(p), "achievetitle.log")
	os.MkdirAll(filepath.Dir(logPath), 0o700)
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > 1<<20 {
		os.Rename(logPath, logPath+".old") // keep it small
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	// File first: in GUI builds stderr may not exist, and MultiWriter stops at the first error.
	log.SetOutput(io.MultiWriter(f, os.Stderr))
}

type app struct {
	store   *Store
	worker  *Worker
	updater *Updater
	chat    *Chat
	srv     *server
	http    *http.Server
	ctx     context.Context
	stop    context.CancelFunc
	port    int
}

func startApp(port int, restarted bool) (*app, bool) {
	store, err := loadStore()
	if err != nil {
		log.Fatal("loading config: ", err)
	}

	// Loopback only: nothing on the network can reach the settings page.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	for i := 0; err != nil && restarted && i < 20; i++ {
		// After an update the old copy may still be releasing the port.
		time.Sleep(250 * time.Millisecond)
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	}
	if err != nil {
		log.Printf("port %d is busy, AchieveTitle is probably already running: %v", port, err)
		return nil, false
	}

	a := &app{store: store, worker: newWorker(store), updater: newUpdater(), port: port}
	a.ctx, a.stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	a.srv = &server{store: store, worker: a.worker, updater: a.updater, base: fmt.Sprintf("http://localhost:%d", port), state: randHex(), botState: randHex(), steamNonce: randHex(), quit: a.stop}
	a.chat = newChat(store, a.worker)
	a.srv.chat = a.chat
	a.worker.onUnlock = a.chat.Announce

	// Keep the OBS script's copy of the app location current (it moves on updates).
	if p, err := scriptPath(); err == nil && fileExists(p) {
		writeOBSScript()
	}
	go a.worker.Run(a.ctx)
	go a.chat.Run(a.ctx)
	go a.updater.Run(a.ctx, func() bool { return store.Get().CheckUpdates })
	a.http = &http.Server{Handler: a.srv.routes(), ReadHeaderTimeout: 10 * time.Second}
	go a.http.Serve(ln)

	log.Printf("AchieveTitle %s running. Settings: %s  |  OBS overlay: %s/overlay", version, a.srv.base, a.srv.base)
	return a, true
}

// wait blocks until the app is asked to stop, then shuts down cleanly
// (and restarts into the new version after an update).
func (a *app) wait() {
	<-a.ctx.Done()
	restart := a.srv.restarting.Load()
	log.Println("shutting down…")
	// Don't flip the title back and forth while an update restarts the app.
	if a.store.Get().RestoreOnExit && !restart {
		a.worker.Restore()
	}
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a.http.Shutdown(sctx)

	if restart {
		cmd := exec.Command(a.updater.exe, "-port", fmt.Sprint(a.port), "-no-browser", "-restarted")
		if err := cmd.Start(); err != nil {
			log.Println("could not restart after update, please start AchieveTitle again:", err)
			return
		}
		log.Println("restarting with the new version…")
	}
}

type server struct {
	store      *Store
	worker     *Worker
	base       string
	state      string // OAuth CSRF state for the channel login
	botState   string // and for the chat bot login
	steamNonce string // ties a Steam sign-in response to this app run
	chat       *Chat
	quit       func()

	updater     *Updater
	restarting  atomic.Bool
	overlayTest atomic.Int64
}

func (s *server) routes() http.Handler {
	static, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /overlay", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "overlay.html")
	})
	mux.HandleFunc("GET /overlay/recent", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "recent.html")
	})
	mux.HandleFunc("GET /overlay/sound", serveSound)
	mux.HandleFunc("POST /api/overlay/sound", s.sameOrigin(s.uploadSound))
	mux.HandleFunc("GET /sounds.js", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "sounds.js")
	})
	mux.HandleFunc("GET /dock", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "dock.html")
	})
	mux.HandleFunc("POST /api/enabled", s.sameOrigin(s.setEnabled))
	mux.HandleFunc("POST /api/quick", s.sameOrigin(s.quickSettings))
	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "callback.html")
	})
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("POST /api/config", s.sameOrigin(s.postConfig))
	mux.HandleFunc("POST /api/refresh", s.sameOrigin(func(w http.ResponseWriter, r *http.Request) {
		s.worker.Kick()
		w.WriteHeader(204)
	}))
	mux.HandleFunc("POST /api/restore", s.sameOrigin(func(w http.ResponseWriter, r *http.Request) {
		s.worker.Restore()
		w.WriteHeader(204)
	}))
	mux.HandleFunc("POST /api/quit", s.sameOrigin(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
		go s.quit()
	}))
	mux.HandleFunc("GET /api/categories", s.searchCategories)
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.updater.Info())
	})
	mux.HandleFunc("POST /api/update/check", s.sameOrigin(func(w http.ResponseWriter, r *http.Request) {
		s.updater.Check(r.Context())
		writeJSON(w, s.updater.Info())
	}))
	mux.HandleFunc("POST /api/update/install", s.sameOrigin(s.installUpdate))
	mux.HandleFunc("GET /api/twitch/login", s.twitchLogin)
	mux.HandleFunc("POST /api/twitch/token", s.sameOrigin(s.twitchToken))
	mux.HandleFunc("POST /api/twitch/logout", s.sameOrigin(s.twitchLogout))
	mux.HandleFunc("POST /api/overlay/test", s.sameOrigin(func(w http.ResponseWriter, r *http.Request) {
		s.overlayTest.Add(1)
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/icons", func(w http.ResponseWriter, r *http.Request) {
		appID, icons := s.worker.Icons()
		writeJSON(w, map[string]any{"app_id": appID, "icons": icons})
	})
	mux.HandleFunc("GET /api/achievements", func(w http.ResponseWriter, r *http.Request) {
		appID, list := s.worker.Locked()
		writeJSON(w, map[string]any{"app_id": appID, "locked": list, "chasing": s.store.Get().Chasing[appID]})
	})
	mux.HandleFunc("GET /api/obs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.obsStatus()) })
	mux.HandleFunc("POST /api/obs/setup", s.sameOrigin(func(w http.ResponseWriter, r *http.Request) {
		if err := s.setupOBS(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		log.Println("OBS set up: script and control panel dock added")
		writeJSON(w, s.obsStatus())
	}))
	mux.HandleFunc("GET /api/steam/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, steamLoginURL(s.base, s.steamReturnTo()), http.StatusFound)
	})
	mux.HandleFunc("GET /auth/steam", s.steamCallback)
	mux.HandleFunc("POST /api/steam/check", s.sameOrigin(s.steamCheck))
	mux.HandleFunc("GET /api/bot/login", s.botLogin)
	mux.HandleFunc("POST /api/bot/logout", s.sameOrigin(s.botLogout))
	return mux
}

// sameOrigin blocks other websites open in the streamer's browser from
// changing settings through this local server.
func (s *server) sameOrigin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		o := r.Header.Get("Origin")
		if o != s.base && o != "http://127.0.0.1"+s.base[len("http://localhost"):] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (s *server) getStatus(w http.ResponseWriter, r *http.Request) {
	st := s.worker.Status()
	st.Chat = s.chat.Status()
	st.OverlayTest = s.overlayTest.Load()
	writeJSON(w, st)
}

// publicConfig never sends secrets back to the browser.
type publicConfig struct {
	SteamID         string        `json:"steam_id"`
	HasSteamKey     bool          `json:"has_steam_key"`
	CustomTitle     string        `json:"custom_title"`
	Template        string        `json:"template"`
	FallbackTmpl    string        `json:"fallback_template"`
	IntervalSeconds int           `json:"interval_seconds"`
	Enabled         bool          `json:"enabled"`
	RestoreOnExit   bool          `json:"restore_on_exit"`
	SetCategory     bool          `json:"set_category"`
	CategoryMode    string        `json:"category_mode"`
	ManualCategory  *Category     `json:"manual_category"`
	ManageTags      bool          `json:"manage_tags"`
	Tags            []string      `json:"tags"`
	CheckUpdates    bool          `json:"check_updates"`
	Bar             BarStyle      `json:"bar"`
	Overlay         OverlayConfig `json:"overlay"`
	TwitchClientID  string        `json:"twitch_client_id"`
	HasBuiltinID    bool          `json:"has_builtin_client_id"`
	TwitchLogin     string        `json:"twitch_login"`
	BotLogin        string        `json:"bot_login"`
	TwitchAvatar    string        `json:"twitch_avatar"`
	BotAvatar       string        `json:"bot_avatar"`
	ChannelCanChat  bool          `json:"channel_can_chat"`
	Chat            ChatConfig    `json:"chat"`
	ChatVars        []string      `json:"chat_vars"`
	Vars            []string      `json:"vars"`
	Version         string        `json:"version"`
	RedirectURI     string        `json:"redirect_uri"`
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	s.fetchAvatars()
	c := s.store.Get()
	writeJSON(w, publicConfig{
		SteamID: c.SteamID, HasSteamKey: c.SteamAPIKey != "", CustomTitle: c.CustomTitle,
		Template: c.Template, FallbackTmpl: c.FallbackTmpl, IntervalSeconds: c.IntervalSeconds,
		Enabled: c.Enabled, RestoreOnExit: c.RestoreOnExit, TwitchClientID: c.TwitchClientID,
		SetCategory: c.SetCategory, CategoryMode: c.CategoryMode, ManualCategory: c.ManualCategory, ManageTags: c.ManageTags, Tags: CleanTags(c.Tags), CheckUpdates: c.CheckUpdates, Bar: c.Bar.clean(), Overlay: c.Overlay.clean(),
		HasBuiltinID: DefaultTwitchClientID != "", TwitchLogin: c.TwitchLogin,
		BotLogin: c.BotLogin, TwitchAvatar: c.TwitchAvatar, BotAvatar: c.BotAvatar, Chat: c.Chat, ChatVars: ChatVars, ChannelCanChat: hasScopes(c.TwitchScopes, botScope),
		Vars: TemplateVars, Version: version, RedirectURI: s.base + "/auth/callback",
	})
}

func (s *server) postConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		publicConfig
		SteamAPIKey string `json:"steam_api_key"` // empty = keep existing
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	wasEnabled := s.store.Get().Enabled
	err := s.store.Update(func(c *Config) {
		if in.SteamAPIKey != "" {
			c.SteamAPIKey = in.SteamAPIKey
		}
		c.SteamID, c.CustomTitle, c.Template, c.FallbackTmpl = in.SteamID, in.CustomTitle, in.Template, in.FallbackTmpl
		c.IntervalSeconds, c.Enabled, c.RestoreOnExit = in.IntervalSeconds, in.Enabled, in.RestoreOnExit
		c.TwitchClientID = in.TwitchClientID
		c.SetCategory, c.ManageTags, c.Tags = in.SetCategory, in.ManageTags, CleanTags(in.Tags)
		c.CategoryMode, c.ManualCategory = "auto", nil
		if in.CategoryMode == "manual" && in.ManualCategory != nil && in.ManualCategory.ID != "" {
			c.CategoryMode, c.ManualCategory = "manual", in.ManualCategory
		}
		c.CheckUpdates = in.CheckUpdates
		c.Bar = in.Bar.clean()
		// The uploaded file's name and version are set by the upload itself.
		ov := in.Overlay
		ov.CustomSound, ov.SoundVer = c.Overlay.CustomSound, c.Overlay.SoundVer
		c.Overlay = ov.clean()
		c.Chat = cleanChatConfig(in.Chat)
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if wasEnabled && !in.Enabled && in.RestoreOnExit {
		go s.worker.Restore()
	}
	s.worker.Kick()
	s.chat.Reload()
	w.WriteHeader(204)
}

// setEnabled is the on/off switch used by the OBS dock.
func (s *server) setEnabled(w http.ResponseWriter, r *http.Request) {
	var in struct{ Enabled bool }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.setEnabledTo(in.Enabled); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(204)
}

// setEnabledTo turns automatic updates on or off (dock, tray and OBS script).
// quickSettings changes a few settings at once (used by the OBS dock). Unlike
// POST /api/config, fields left out are kept as they are.
func (s *server) quickSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled     *bool          `json:"enabled"`
		ChatEnabled *bool          `json:"chat_enabled"`
		SetCategory *bool          `json:"set_category"`
		CustomTitle *string        `json:"custom_title"`
		Template    *string        `json:"template"`
		Bar         *BarStyle      `json:"bar"`
		Overlay     *OverlayConfig `json:"overlay"`
		Chasing     *string        `json:"chasing"` // API name of an achievement in the current game; "" stops chasing
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if in.Enabled != nil {
		if err := s.setEnabledTo(*in.Enabled); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	err := s.store.Update(func(c *Config) {
		if in.ChatEnabled != nil {
			c.Chat.Enabled = *in.ChatEnabled
		}
		if in.SetCategory != nil {
			c.SetCategory = *in.SetCategory
		}
		if in.CustomTitle != nil {
			c.CustomTitle = strings.TrimSpace(*in.CustomTitle)
		}
		if in.Template != nil && strings.TrimSpace(*in.Template) != "" {
			c.Template = strings.TrimSpace(*in.Template)
		}
		if in.Bar != nil {
			c.Bar = in.Bar.clean()
		}
		if in.Chasing != nil {
			if appID, _ := s.worker.Locked(); appID != "" {
				if c.Chasing == nil {
					c.Chasing = map[string]string{}
				}
				if *in.Chasing == "" {
					delete(c.Chasing, appID)
				} else {
					c.Chasing[appID] = *in.Chasing
				}
			}
		}
		if in.Overlay != nil {
			ov := *in.Overlay
			ov.CustomSound, ov.SoundVer = c.Overlay.CustomSound, c.Overlay.SoundVer
			c.Overlay = ov.clean()
		}
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.worker.Kick()
	s.chat.Reload()
	w.WriteHeader(204)
}

func (s *server) setEnabledTo(on bool) error {
	was := s.store.Get()
	if err := s.store.Update(func(c *Config) { c.Enabled = on }); err != nil {
		return err
	}
	if was.Enabled && !on && was.RestoreOnExit {
		go s.worker.Restore()
	}
	s.worker.Kick()
	return nil
}

func (s *server) twitchLogin(w http.ResponseWriter, r *http.Request) {
	id := clientID(s.store.Get())
	if id == "" {
		http.Error(w, "Set a Twitch Client ID in settings first.", 400)
		return
	}
	http.Redirect(w, r, twitchAuthURL(id, s.base+"/auth/callback", s.state, twitchScope), http.StatusFound)
}

// botLogin starts the Twitch login for the chat bot account. force_verify
// shows Twitch's "Not you?" link so a different account can be picked.
func (s *server) botLogin(w http.ResponseWriter, r *http.Request) {
	id := clientID(s.store.Get())
	if id == "" {
		http.Error(w, "Set a Twitch Client ID in settings first.", 400)
		return
	}
	http.Redirect(w, r, twitchAuthURL(id, s.base+"/auth/callback", s.botState, botScope), http.StatusFound)
}

func (s *server) steamReturnTo() string { return s.base + "/auth/steam?n=" + s.steamNonce }

// steamCallback is where "Sign in with Steam" comes back to.
func (s *server) steamCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := ""
	err := fmt.Errorf("Steam sign-in expired, please try again")
	if q.Get("n") == s.steamNonce {
		id, err = s.worker.steam.VerifySteamLogin(r.Context(), q, s.steamReturnTo())
	}
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><body style="background:#0b0a10;color:#f1eff7;font:16px system-ui;display:grid;place-items:center;height:100vh;margin:0"><div style="text-align:center"><p>%s</p><p><a style="color:#bf94ff" href="/">Back to AchieveTitle</a></p></div>`, html.EscapeString(err.Error()))
		return
	}
	s.store.Update(func(c *Config) { c.SteamID = id })
	s.worker.Kick()
	http.Redirect(w, r, "/#steam-linked", http.StatusFound)
}

// steamCheck tells the settings page whether the Steam key works and which account is linked.
func (s *server) steamCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key string `json:"key"`
		ID  string `json:"steam_id"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
	cfg := s.store.Get()
	key, id := strings.TrimSpace(in.Key), strings.TrimSpace(in.ID)
	if key == "" {
		key = cfg.SteamAPIKey
	}
	if id == "" {
		id = cfg.SteamID
	}
	type result struct {
		KeyOK   bool          `json:"key_ok"`
		Profile *SteamProfile `json:"profile,omitempty"`
		Error   string        `json:"error,omitempty"`
	}
	if key == "" {
		writeJSON(w, result{Error: "Add your Steam Web API key"})
		return
	}
	lookup := id
	if lookup == "" {
		lookup = "76561197960287930" // a known public account, just to test the key
	} else if resolved, err := s.worker.steam.ResolveID(r.Context(), key, id); err == nil {
		lookup = resolved
	} else {
		writeJSON(w, result{KeyOK: !strings.Contains(err.Error(), "API key"), Error: err.Error()})
		return
	}
	p, err := s.worker.steam.Profile(r.Context(), key, lookup)
	if err != nil {
		writeJSON(w, result{Error: err.Error()})
		return
	}
	res := result{KeyOK: true}
	if id != "" {
		res.Profile = &p
	}
	writeJSON(w, res)
}

func (s *server) botLogout(w http.ResponseWriter, r *http.Request) {
	s.store.Update(func(c *Config) { c.BotToken, c.BotUserID, c.BotLogin, c.BotAvatar = "", "", "", "" })
	s.chat.Reload()
	w.WriteHeader(204)
}

func (s *server) twitchToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token, State string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.Token == "" {
		http.Error(w, "bad request", 400)
		return
	}
	if in.State != s.state && in.State != s.botState {
		http.Error(w, "login expired, please try again", 400)
		return
	}
	uid, login, scopes, err := s.worker.twitch.Validate(r.Context(), in.Token)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if in.State == s.botState {
		if !hasScopes(scopes, botScope) {
			http.Error(w, "the bot account didn't grant chat permissions, please try again", 400)
			return
		}
		s.store.Update(func(c *Config) { c.BotToken, c.BotUserID, c.BotLogin, c.BotAvatar = in.Token, uid, login, "" })
		s.chat.Reload()
		writeJSON(w, map[string]string{"login": login, "account": "bot"})
		return
	}
	s.store.Update(func(c *Config) {
		c.TwitchToken, c.TwitchUserID, c.TwitchLogin, c.TwitchScopes, c.TwitchAvatar = in.Token, uid, login, scopes, ""
	})
	s.worker.Kick()
	s.chat.Reload()
	writeJSON(w, map[string]string{"login": login, "account": "channel"})
}

func (s *server) twitchLogout(w http.ResponseWriter, r *http.Request) {
	s.worker.Restore()
	s.store.Update(func(c *Config) {
		c.TwitchToken, c.TwitchUserID, c.TwitchLogin, c.TwitchAvatar, c.Enabled = "", "", "", "", false
	})
	w.WriteHeader(204)
}

// searchCategories backs the "choose my game" picker.
func (s *server) searchCategories(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	cfg := s.store.Get()
	if q == "" {
		writeJSON(w, []Category{})
		return
	}
	if cfg.TwitchToken == "" {
		http.Error(w, "connect Twitch first", 400)
		return
	}
	res, err := s.worker.twitch.SearchCategories(r.Context(), cfg, q)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	writeJSON(w, res)
}

func (s *server) installUpdate(w http.ResponseWriter, r *http.Request) {
	if err := s.updater.Install(r.Context()); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	log.Println("update installed")
	w.WriteHeader(204)
	// Restart into the new version once this response has gone out.
	s.restarting.Store(true)
	go func() {
		time.Sleep(300 * time.Millisecond)
		s.quit()
	}()
}

func randHex() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		log.Println("open this in your browser:", u)
	}
}

// fetchAvatars looks up the profile pictures of connected Twitch accounts that
// don't have one saved yet (just connected, or connected before this existed).
func (s *server) fetchAvatars() {
	c := s.store.Get()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if c.TwitchToken != "" && c.TwitchAvatar == "" {
		if a, err := s.worker.twitch.Avatar(ctx, c.TwitchToken, clientID(c), c.TwitchUserID); err == nil {
			s.store.Update(func(c *Config) { c.TwitchAvatar = a })
		}
	}
	if c.BotToken != "" && c.BotAvatar == "" {
		if a, err := s.worker.twitch.Avatar(ctx, c.BotToken, clientID(c), c.BotUserID); err == nil {
			s.store.Update(func(c *Config) { c.BotAvatar = a })
		}
	}
}
