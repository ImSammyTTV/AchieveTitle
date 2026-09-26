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
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

var version = "dev"

func main() {
	port := flag.Int("port", 7878, "port for the local settings page")
	noBrowser := flag.Bool("no-browser", false, "don't open the settings page on start")
	flag.Parse()

	store, err := loadStore()
	if err != nil {
		log.Fatal("loading config: ", err)
	}
	worker := newWorker(store)

	// Loopback only: nothing on the network can reach the settings page.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		// Most likely already running (e.g. launched twice from the app menu):
		// just show the existing settings page.
		log.Printf("port %d is busy, AchieveTitle is probably already running: %v", *port, err)
		if !*noBrowser {
			openBrowser(fmt.Sprintf("http://localhost:%d", *port))
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &server{store: store, worker: worker, base: fmt.Sprintf("http://localhost:%d", *port), state: randHex(), quit: stop}

	go worker.Run(ctx)
	httpSrv := &http.Server{Handler: srv.routes(), ReadHeaderTimeout: 10 * time.Second}
	go httpSrv.Serve(ln)

	log.Printf("AchieveTitle %s running. Settings: %s  |  OBS overlay: %s/overlay", version, srv.base, srv.base)
	log.Println("Press Ctrl+C to stop.")
	if !*noBrowser {
		openBrowser(srv.base)
	}

	<-ctx.Done()
	log.Println("shutting down…")
	if store.Get().RestoreOnExit {
		worker.Restore()
	}
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	httpSrv.Shutdown(sctx)
}

type server struct {
	store  *Store
	worker *Worker
	base   string
	state  string // OAuth CSRF state
	quit   func()
}

func (s *server) routes() http.Handler {
	static, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /overlay", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "overlay.html")
	})
	mux.HandleFunc("GET /dock", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "dock.html")
	})
	mux.HandleFunc("POST /api/enabled", s.sameOrigin(s.setEnabled))
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
	mux.HandleFunc("GET /api/twitch/login", s.twitchLogin)
	mux.HandleFunc("POST /api/twitch/token", s.sameOrigin(s.twitchToken))
	mux.HandleFunc("POST /api/twitch/logout", s.sameOrigin(s.twitchLogout))
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
	writeJSON(w, s.worker.Status())
}

// publicConfig never sends secrets back to the browser.
type publicConfig struct {
	SteamID         string   `json:"steam_id"`
	HasSteamKey     bool     `json:"has_steam_key"`
	CustomTitle     string   `json:"custom_title"`
	Template        string   `json:"template"`
	FallbackTmpl    string   `json:"fallback_template"`
	IntervalSeconds int      `json:"interval_seconds"`
	Enabled         bool     `json:"enabled"`
	RestoreOnExit   bool     `json:"restore_on_exit"`
	SetCategory     bool     `json:"set_category"`
	ManageTags      bool     `json:"manage_tags"`
	Tags            []string `json:"tags"`
	TwitchClientID  string   `json:"twitch_client_id"`
	HasBuiltinID    bool     `json:"has_builtin_client_id"`
	TwitchLogin     string   `json:"twitch_login"`
	Vars            []string `json:"vars"`
	Version         string   `json:"version"`
	RedirectURI     string   `json:"redirect_uri"`
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	c := s.store.Get()
	writeJSON(w, publicConfig{
		SteamID: c.SteamID, HasSteamKey: c.SteamAPIKey != "", CustomTitle: c.CustomTitle,
		Template: c.Template, FallbackTmpl: c.FallbackTmpl, IntervalSeconds: c.IntervalSeconds,
		Enabled: c.Enabled, RestoreOnExit: c.RestoreOnExit, TwitchClientID: c.TwitchClientID,
		SetCategory: c.SetCategory, ManageTags: c.ManageTags, Tags: CleanTags(c.Tags),
		HasBuiltinID: DefaultTwitchClientID != "", TwitchLogin: c.TwitchLogin,
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
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if wasEnabled && !in.Enabled && in.RestoreOnExit {
		go s.worker.Restore()
	}
	s.worker.Kick()
	w.WriteHeader(204)
}

// setEnabled is the on/off switch used by the OBS dock.
func (s *server) setEnabled(w http.ResponseWriter, r *http.Request) {
	var in struct{ Enabled bool }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	cfg := s.store.Get()
	if err := s.store.Update(func(c *Config) { c.Enabled = in.Enabled }); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if cfg.Enabled && !in.Enabled && cfg.RestoreOnExit {
		go s.worker.Restore()
	}
	s.worker.Kick()
	w.WriteHeader(204)
}

func (s *server) twitchLogin(w http.ResponseWriter, r *http.Request) {
	id := clientID(s.store.Get())
	if id == "" {
		http.Error(w, "Set a Twitch Client ID in settings first.", 400)
		return
	}
	http.Redirect(w, r, twitchAuthURL(id, s.base+"/auth/callback", s.state), http.StatusFound)
}

func (s *server) twitchToken(w http.ResponseWriter, r *http.Request) {
	var in struct{ Token, State string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil || in.Token == "" {
		http.Error(w, "bad request", 400)
		return
	}
	if in.State != s.state {
		http.Error(w, "login expired, please try again", 400)
		return
	}
	uid, login, err := s.worker.twitch.Validate(r.Context(), in.Token)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.store.Update(func(c *Config) { c.TwitchToken, c.TwitchUserID, c.TwitchLogin = in.Token, uid, login })
	s.worker.Kick()
	writeJSON(w, map[string]string{"login": login})
}

func (s *server) twitchLogout(w http.ResponseWriter, r *http.Request) {
	s.worker.Restore()
	s.store.Update(func(c *Config) { c.TwitchToken, c.TwitchUserID, c.TwitchLogin, c.Enabled = "", "", "", false })
	w.WriteHeader(204)
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
