package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// OverlayConfig controls the OBS overlay's unlock celebration.
type OverlayConfig struct {
	Animation   string `json:"animation"`    // "confetti", "glow" or "simple"
	Sound       string `json:"sound"`        // "none", "chime", "fanfare" or "custom"
	Volume      int    `json:"volume"`       // 0-100
	CustomSound string `json:"custom_sound"` // original name of the uploaded file
	SoundVer    int    `json:"sound_ver"`    // bumped on upload so the overlay reloads it
	BarAnim     string `json:"bar_anim"`     // progress bar: "none", "shimmer" or "flow"
}

func defaultOverlay() OverlayConfig {
	return OverlayConfig{Animation: "confetti", Sound: "chime", Volume: 70, BarAnim: "shimmer"}
}

func (o OverlayConfig) clean() OverlayConfig {
	switch o.Animation {
	case "confetti", "glow", "simple":
	default:
		o.Animation = "confetti"
	}
	switch o.Sound {
	case "none", "chime", "fanfare":
	case "custom":
		if o.CustomSound == "" {
			o.Sound = "chime"
		}
	default:
		o.Sound = "chime"
	}
	switch o.BarAnim {
	case "none", "shimmer", "flow":
	default:
		o.BarAnim = "shimmer"
	}
	o.Volume = min(max(o.Volume, 0), 100)
	return o
}

const maxSoundBytes = 3 << 20 // 3 MB is plenty for a short unlock sound

var soundTypes = map[string]string{".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".m4a": "audio/mp4"}

func soundDir() (string, error) {
	p, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// findSound returns the uploaded unlock sound, if there is one.
func findSound() (path, contentType string) {
	dir, err := soundDir()
	if err != nil {
		return "", ""
	}
	for ext, ct := range soundTypes {
		if p := filepath.Join(dir, "overlay-sound"+ext); fileExists(p) {
			return p, ct
		}
	}
	return "", ""
}

// uploadSound stores a custom unlock sound next to the settings.
func (s *server) uploadSound(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSoundBytes+64<<10)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "please choose a sound file under 3 MB", 400)
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if _, ok := soundTypes[ext]; !ok {
		http.Error(w, "please use an MP3, WAV, OGG or M4A file", 400)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSoundBytes+1))
	if err != nil || len(data) > maxSoundBytes {
		http.Error(w, "please choose a sound file under 3 MB", 400)
		return
	}
	dir, err := soundDir()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for e := range soundTypes {
		os.Remove(filepath.Join(dir, "overlay-sound"+e))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "overlay-sound"+ext), data, 0o600); err != nil {
		http.Error(w, fmt.Sprintf("saving the sound: %v", err), 500)
		return
	}
	name := filepath.Base(hdr.Filename)
	s.store.Update(func(c *Config) {
		c.Overlay.CustomSound, c.Overlay.Sound = name, "custom"
		c.Overlay.SoundVer++
	})
	writeJSON(w, map[string]string{"name": name})
}

func serveSound(w http.ResponseWriter, r *http.Request) {
	p, ct := findSound()
	if p == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, p)
}
