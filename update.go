package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// updateAPI is GitHub's "latest release" endpoint; it never returns pre-releases.
// Overridable at build time for testing.
var updateAPI = "https://api.github.com/repos/ImSammyTTV/AchieveTitle/releases/latest"

// UpdateInfo is what the settings page shows about updates.
type UpdateInfo struct {
	Current      string    `json:"current"`
	Latest       string    `json:"latest"`
	Available    bool      `json:"available"`
	PageURL      string    `json:"page_url"`
	CanInstall   bool      `json:"can_install"`
	CantInstall  string    `json:"cant_install"` // why not, shown to the user
	Installing   bool      `json:"installing"`
	Error        string    `json:"error"`
	LastChecked  time.Time `json:"last_checked"`
	assetURL     string
	checksumsURL string
	assetName    string
}

type Updater struct {
	mu   sync.Mutex
	info UpdateInfo
	http *http.Client
	exe  string // path of the running executable
}

func newUpdater() *Updater {
	u := &Updater{http: &http.Client{Timeout: 5 * time.Minute}}
	u.info.Current = version
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		u.info.CantInstall = "can't locate the running app"
		return u
	}
	u.exe = exe
	os.Remove(exe + ".old") // left over from the previous update (Windows can't delete a running exe)
	u.info.CanInstall, u.info.CantInstall = canReplace(exe)
	return u
}

// canReplace decides whether AchieveTitle may overwrite itself. Package-manager
// and Flatpak installs must be updated by the package manager instead.
func canReplace(exe string) (bool, string) {
	if version == "dev" {
		return false, "this is a development build"
	}
	if os.Getenv("FLATPAK_ID") != "" {
		return false, "installed with Flatpak: update it from your software store"
	}
	if runtime.GOOS == "linux" && (strings.HasPrefix(exe, "/usr/") || strings.HasPrefix(exe, "/opt/")) {
		return false, "installed with your package manager: update it the same way you installed it"
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".achievetitle-update-check-*")
	if err != nil {
		return false, "the app's folder isn't writable: download the new version manually"
	}
	f.Close()
	os.Remove(f.Name())
	return true, ""
}

func (u *Updater) Info() UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.info
}

// Run checks for updates at start and then once a day.
func (u *Updater) Run(ctx context.Context, enabled func() bool) {
	for {
		if enabled() && version != "dev" {
			u.Check(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

func (u *Updater) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", updateAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AchieveTitle/"+version)
	resp, err := u.http.Do(req)
	if err != nil {
		return u.fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return u.fail(fmt.Errorf("update check: HTTP %d", resp.StatusCode))
	}
	var rel struct {
		Tag     string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return u.fail(err)
	}

	u.mu.Lock()
	defer u.mu.Unlock()
	u.info.LastChecked, u.info.Error = time.Now(), ""
	u.info.Latest, u.info.PageURL = rel.Tag, rel.HTMLURL
	u.info.Available = newerVersion(rel.Tag, version)
	u.info.assetURL, u.info.checksumsURL = "", ""
	u.info.assetName = fmt.Sprintf("AchieveTitle-%s-%s-%s.zip", rel.Tag, runtime.GOOS, runtime.GOARCH)
	for _, a := range rel.Assets {
		switch a.Name {
		case u.info.assetName:
			u.info.assetURL = a.URL
		case "SHA256SUMS.txt":
			u.info.checksumsURL = a.URL
		}
	}
	return nil
}

func (u *Updater) fail(err error) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.info.Error = err.Error()
	u.info.LastChecked = time.Now()
	return err
}

// Install downloads the new release, verifies its checksum and swaps it in
// place of the running executable. The caller restarts the app afterwards.
func (u *Updater) Install(ctx context.Context) error {
	u.mu.Lock()
	info := u.info
	if info.Installing {
		u.mu.Unlock()
		return errors.New("an update is already being installed")
	}
	u.info.Installing, u.info.Error = true, ""
	u.mu.Unlock()

	err := u.install(ctx, info)
	u.mu.Lock()
	u.info.Installing = false
	if err != nil {
		u.info.Error = err.Error()
	}
	u.mu.Unlock()
	return err
}

func (u *Updater) install(ctx context.Context, info UpdateInfo) error {
	switch {
	case !info.Available:
		return errors.New("no update available")
	case !info.CanInstall:
		return errors.New(info.CantInstall)
	case info.assetURL == "" || info.checksumsURL == "":
		return fmt.Errorf("the %s release has no download for %s/%s", info.Latest, runtime.GOOS, runtime.GOARCH)
	}

	sums, err := u.download(ctx, info.checksumsURL, 1<<20)
	if err != nil {
		return err
	}
	want := checksumFor(sums, info.assetName)
	if want == "" {
		return fmt.Errorf("%s is missing from the release checksums", info.assetName)
	}
	archive, err := u.download(ctx, info.assetURL, 100<<20)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return errors.New("downloaded update is corrupted (checksum mismatch), nothing was changed")
	}
	bin, err := extractBinary(archive)
	if err != nil {
		return err
	}
	return replaceExecutable(u.exe, bin)
}

func (u *Updater) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", "AchieveTitle/"+version)
	resp, err := u.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("downloading update: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func checksumFor(sums []byte, name string) string {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

func extractBinary(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("reading update: %w", err)
	}
	for _, f := range zr.File {
		if base := filepath.Base(f.Name); base == "AchieveTitle" || base == "AchieveTitle.exe" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 200<<20))
		}
	}
	return nil, errors.New("update archive doesn't contain the app")
}

// replaceExecutable writes the new binary next to the old one and swaps them.
// Renaming a running executable is allowed on Windows, macOS and Linux.
func replaceExecutable(exe string, bin []byte) error {
	newPath, oldPath := exe+".new", exe+".old"
	if err := os.WriteFile(newPath, bin, 0o755); err != nil {
		return fmt.Errorf("saving update: %w", err)
	}
	os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("replacing app: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(oldPath, exe) // put the working version back
		return fmt.Errorf("replacing app: %w", err)
	}
	if runtime.GOOS != "windows" {
		os.Remove(oldPath)
	}
	return nil
}

// newerVersion reports whether tag (e.g. "v0.3.0") is newer than current.
func newerVersion(tag, current string) bool {
	a, okA := parseVersion(tag)
	b, okB := parseVersion(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
