package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed obs/achievetitle.lua
var obsScript string

// obsConfigDirs lists where OBS keeps its settings on this system (Flatpak too).
var obsConfigDirs = func() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return []string{filepath.Join(os.Getenv("APPDATA"), "obs-studio")}
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "obs-studio")}
	default:
		cfg, _ := os.UserConfigDir()
		return []string{filepath.Join(cfg, "obs-studio"), filepath.Join(home, ".var", "app", "com.obsproject.Studio", "config", "obs-studio")}
	}
}

// obsRunning reports whether OBS is open. OBS rewrites its settings when it
// closes, so changes made while it runs would be lost.
var obsRunning = func() bool {
	var out []byte
	switch runtime.GOOS {
	case "windows":
		out, _ = exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
		s := strings.ToLower(string(out))
		return strings.Contains(s, `"obs64.exe"`) || strings.Contains(s, `"obs32.exe"`)
	case "darwin":
		return exec.Command("pgrep", "-x", "OBS").Run() == nil
	default:
		return exec.Command("pgrep", "-x", "obs").Run() == nil
	}
}

// OBSStatus is shown in the OBS tab.
type OBSStatus struct {
	Found         bool   `json:"found"`
	Running       bool   `json:"running"`
	ScriptPath    string `json:"script_path"`
	ScriptAdded   bool   `json:"script_added"`
	DockAdded     bool   `json:"dock_added"`
	SceneCollName string `json:"scene_collection"`
}

func findOBS() (dir string, ok bool) {
	for _, d := range obsConfigDirs() {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d, true
		}
	}
	return "", false
}

// obsUserINI is user.ini in OBS 31+, global.ini before that.
func obsUserINI(dir string) string {
	if p := filepath.Join(dir, "user.ini"); fileExists(p) {
		return p
	}
	return filepath.Join(dir, "global.ini")
}

// sceneCollectionPath finds the scene collection OBS opens with. Newer OBS
// versions store the name with ".json" already on it, older ones without.
func sceneCollectionPath(dir, ini string) (path, name string) {
	name = strings.TrimSuffix(iniGet(ini, "Basic", "SceneCollectionFile"), ".json")
	if name == "" {
		return "", ""
	}
	return filepath.Join(dir, "basic", "scenes", name+".json"), name
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// scriptPath is where the ready-to-use OBS script is written: next to the
// AchieveTitle settings, so it survives app updates and moves.
func scriptPath() (string, error) {
	p, err := configPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "obs", "achievetitle.lua"), nil
}

// writeOBSScript saves the OBS script with this app's location filled in.
// It's refreshed on every start, so the path stays right after updates.
func writeOBSScript() (string, error) {
	p, err := scriptPath()
	if err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	lua := strings.Replace(obsScript, `local default_exe = ""`, "local default_exe = "+luaString(exe), 1)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, []byte(lua), 0o644)
}

func luaString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s) + `"`
}

func (s *server) obsStatus() OBSStatus {
	st := OBSStatus{Running: obsRunning()}
	st.ScriptPath, _ = scriptPath()
	dir, ok := findOBS()
	if !ok {
		return st
	}
	st.Found = true
	ini, _ := os.ReadFile(obsUserINI(dir))
	st.DockAdded = strings.Contains(iniGet(string(ini), "BasicWindow", "ExtraBrowserDocks"), s.base+"/dock")
	collPath, coll := sceneCollectionPath(dir, string(ini))
	st.SceneCollName = coll
	if collPath != "" {
		b, _ := os.ReadFile(collPath)
		st.ScriptAdded = strings.Contains(string(b), "achievetitle.lua")
	}
	return st
}

// setupOBS adds the AchieveTitle script to the current scene collection and the
// control panel as a custom browser dock. Every file it changes is backed up.
func (s *server) setupOBS() error {
	if obsRunning() {
		return errors.New("please close OBS first, then click the button again")
	}
	dir, ok := findOBS()
	if !ok {
		return errors.New("couldn't find OBS on this computer. Open OBS once, close it, then try again")
	}
	script, err := writeOBSScript()
	if err != nil {
		return fmt.Errorf("saving the OBS script: %w", err)
	}

	iniPath := obsUserINI(dir)
	iniBytes, err := os.ReadFile(iniPath)
	if err != nil {
		return fmt.Errorf("reading OBS settings: %w", err)
	}
	ini := string(iniBytes)

	// 1. The control panel dock.
	type dock struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		UUID  string `json:"uuid"`
	}
	var docks []dock
	if raw := iniGet(ini, "BasicWindow", "ExtraBrowserDocks"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &docks); err != nil {
			return fmt.Errorf("OBS's dock list looks unusual, so nothing was changed: %w", err)
		}
	}
	hasDock := false
	for _, d := range docks {
		hasDock = hasDock || d.URL == s.base+"/dock"
	}
	if !hasDock {
		docks = append(docks, dock{Title: "AchieveTitle", URL: s.base + "/dock", UUID: newUUID()})
		b, _ := json.Marshal(docks)
		newINI := iniSet(ini, "BasicWindow", "ExtraBrowserDocks", string(b))
		if err := writeWithBackup(iniPath, []byte(newINI)); err != nil {
			return err
		}
		ini = newINI
	}

	// 2. The script, in the scene collection OBS opens with.
	collPath, _ := sceneCollectionPath(dir, ini)
	if collPath == "" {
		return errors.New("couldn't tell which scene collection OBS uses. Open OBS once, close it, then try again")
	}
	b, err := os.ReadFile(collPath)
	if err != nil {
		return fmt.Errorf("reading your OBS scene collection: %w", err)
	}
	var scenes map[string]any
	if err := json.Unmarshal(b, &scenes); err != nil {
		return fmt.Errorf("your OBS scene collection looks unusual, so nothing was changed: %w", err)
	}
	modules, _ := scenes["modules"].(map[string]any)
	if modules == nil {
		modules = map[string]any{}
	}
	scripts, _ := modules["scripts-tool"].([]any)
	for _, sc := range scripts {
		if m, ok := sc.(map[string]any); ok {
			if p, _ := m["path"].(string); strings.EqualFold(filepath.Base(p), "achievetitle.lua") {
				return nil // already added
			}
		}
	}
	modules["scripts-tool"] = append(scripts, map[string]any{"path": filepath.ToSlash(script), "settings": map[string]any{}})
	scenes["modules"] = modules
	out, err := json.MarshalIndent(scenes, "", "    ")
	if err != nil {
		return err
	}
	return writeWithBackup(collPath, out)
}

// writeWithBackup keeps the original as "<file>.achievetitle-backup" (once),
// then replaces the file.
func writeWithBackup(path string, data []byte) error {
	backup := path + ".achievetitle-backup"
	if !fileExists(backup) {
		orig, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(backup, orig, 0o644); err != nil {
			return fmt.Errorf("backing up %s: %w", filepath.Base(path), err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// iniGet and iniSet do just enough INI handling for OBS's settings files,
// leaving every other line exactly as it was.
func iniGet(ini, section, key string) string {
	in := false
	for _, line := range strings.Split(ini, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			in = t == "["+section+"]"
			continue
		}
		if in {
			if k, v, ok := strings.Cut(t, "="); ok && k == key {
				return v
			}
		}
	}
	return ""
}

func iniSet(ini, section, key, value string) string {
	nl := "\n"
	if strings.Contains(ini, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(ini, "\r\n", "\n"), "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			if start >= 0 {
				end = i
				break
			}
			if t == "["+section+"]" {
				start = i
			}
		}
	}
	entry := key + "=" + value
	if start < 0 { // no such section yet
		trimmed := strings.TrimRight(strings.Join(lines, nl), nl)
		return trimmed + nl + nl + "[" + section + "]" + nl + entry + nl
	}
	for i := start + 1; i < end; i++ {
		if k, _, ok := strings.Cut(strings.TrimSpace(lines[i]), "="); ok && k == key {
			lines[i] = entry
			return strings.Join(lines, nl)
		}
	}
	// Insert after the section's last non-empty line.
	at := end
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	lines = append(lines[:at], append([]string{entry}, lines[at:]...)...)
	return strings.Join(lines, nl)
}

func newUUID() string {
	h := randHex() // 32 hex characters
	return h[0:8] + "-" + h[8:12] + "-4" + h[13:16] + "-a" + h[17:20] + "-" + h[20:32]
}
