package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeOBS(t *testing.T) (dir string) { return fakeOBSNamed(t, "My_Scenes") }

// fakeOBSNamed writes SceneCollectionFile=<name>; newer OBS versions put ".json" on the end.
func fakeOBSNamed(t *testing.T, name string) (dir string) {
	dir = t.TempDir()
	os.MkdirAll(filepath.Join(dir, "basic", "scenes"), 0o755)
	// Windows line endings and an existing dock, like a real OBS install.
	ini := "[General]\r\nFirstRun=true\r\n\r\n[Basic]\r\nProfile=Untitled\r\nSceneCollectionFile=" + name + "\r\n\r\n[BasicWindow]\r\n" +
		`ExtraBrowserDocks=[{"title":"Chat","url":"https://twitch.tv/popout/chat","uuid":"x"}]` + "\r\nDockState=abc\r\n"
	os.WriteFile(filepath.Join(dir, "user.ini"), []byte(ini), 0o644)
	os.WriteFile(filepath.Join(dir, "basic", "scenes", "My_Scenes.json"),
		[]byte(`{"name":"My Scenes","sources":[{"name":"Webcam"}],"modules":{"scripts-tool":[{"path":"C:/other.lua","settings":{}}]}}`), 0o644)
	return dir
}

func TestSetupOBS(t *testing.T) {
	dir := fakeOBS(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", os.Getenv("XDG_CONFIG_HOME"))
	obsConfigDirs = func() []string { return []string{filepath.Join(dir, "missing"), dir} }
	running := true
	obsRunning = func() bool { return running }
	s := &server{base: "http://localhost:7878"}

	if err := s.setupOBS(); err == nil || !strings.Contains(err.Error(), "close OBS") {
		t.Fatalf("must refuse while OBS is open, got %v", err)
	}
	running = false
	for i := 0; i < 2; i++ { // twice: the second run must not add duplicates
		if err := s.setupOBS(); err != nil {
			t.Fatal(err)
		}
	}

	ini, _ := os.ReadFile(filepath.Join(dir, "user.ini"))
	if !strings.Contains(string(ini), "DockState=abc\r\n") || !strings.Contains(string(ini), "FirstRun=true\r\n") {
		t.Fatalf("other OBS settings were changed:\n%s", ini)
	}
	var docks []map[string]string
	json.Unmarshal([]byte(iniGet(string(ini), "BasicWindow", "ExtraBrowserDocks")), &docks)
	if len(docks) != 2 || docks[0]["title"] != "Chat" || docks[1]["url"] != "http://localhost:7878/dock" || len(docks[1]["uuid"]) != 36 {
		t.Fatalf("docks: %v", docks)
	}

	var scenes map[string]any
	b, _ := os.ReadFile(filepath.Join(dir, "basic", "scenes", "My_Scenes.json"))
	json.Unmarshal(b, &scenes)
	scripts := scenes["modules"].(map[string]any)["scripts-tool"].([]any)
	if len(scripts) != 2 || !strings.HasSuffix(scripts[1].(map[string]any)["path"].(string), "achievetitle.lua") {
		t.Fatalf("scripts: %v", scripts)
	}
	if scenes["sources"] == nil || scenes["name"] != "My Scenes" {
		t.Fatal("scene collection content was lost")
	}

	for _, f := range []string{"user.ini", "basic/scenes/My_Scenes.json"} {
		if !fileExists(filepath.Join(dir, f+".achievetitle-backup")) {
			t.Fatalf("no backup of %s", f)
		}
	}

	script, _ := scriptPath()
	lua, _ := os.ReadFile(script)
	exe, _ := os.Executable()
	if !strings.Contains(string(lua), "local default_exe = \"") || !strings.Contains(string(lua), filepath.Base(exe)) {
		t.Fatal("script doesn't know where the app is")
	}

	st := s.obsStatus()
	if !st.Found || !st.ScriptAdded || !st.DockAdded {
		t.Fatalf("status: %+v", st)
	}
}

func TestSetupOBSNotInstalled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	obsConfigDirs = func() []string { return []string{filepath.Join(t.TempDir(), "nope")} }
	obsRunning = func() bool { return false }
	if err := (&server{base: "http://localhost:7878"}).setupOBS(); err == nil || !strings.Contains(err.Error(), "couldn't find OBS") {
		t.Fatalf("got %v", err)
	}
}

func TestINISetAddsMissingSection(t *testing.T) {
	got := iniSet("[General]\nA=1\n", "BasicWindow", "ExtraBrowserDocks", "[]")
	if got != "[General]\nA=1\n\n[BasicWindow]\nExtraBrowserDocks=[]\n" {
		t.Fatalf("got %q", got)
	}
}

func TestLuaString(t *testing.T) {
	if got := luaString(`C:\Program Files\AchieveTitle\AchieveTitle.exe`); got != `"C:\\Program Files\\AchieveTitle\\AchieveTitle.exe"` {
		t.Fatalf("got %s", got)
	}
}

func TestSetupOBSNewerNaming(t *testing.T) {
	dir := fakeOBSNamed(t, "My_Scenes.json") // how current OBS on macOS stores it
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", os.Getenv("XDG_CONFIG_HOME"))
	obsConfigDirs = func() []string { return []string{dir} }
	obsRunning = func() bool { return false }
	s := &server{base: "http://localhost:7878"}
	if err := s.setupOBS(); err != nil {
		t.Fatal(err)
	}
	if st := s.obsStatus(); !st.ScriptAdded || st.SceneCollName != "My_Scenes" {
		t.Fatalf("status: %+v", st)
	}
}
