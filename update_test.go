package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		tag, cur string
		want     bool
	}{
		{"v0.3.0", "v0.2.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.3.0", "dev", false},
		{"garbage", "v0.2.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.tag, c.cur); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.tag, c.cur, got)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sums := []byte("abc123  AchieveTitle-v0.3.0-linux-amd64.zip\nDEF456  SHA256SUMS.txt\n")
	if got := checksumFor(sums, "AchieveTitle-v0.3.0-linux-amd64.zip"); got != "abc123" {
		t.Fatalf("got %q", got)
	}
	if checksumFor(sums, "missing.zip") != "" {
		t.Fatal("expected no checksum")
	}
}

func TestExtractAndReplace(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("AchieveTitle-v9.9.9-linux-amd64/AchieveTitle")
	w.Write([]byte("new version"))
	zw.Close()

	bin, err := extractBinary(buf.Bytes())
	if err != nil || string(bin) != "new version" {
		t.Fatalf("extract: %q %v", bin, err)
	}
	exe := filepath.Join(t.TempDir(), "AchieveTitle")
	os.WriteFile(exe, []byte("old version"), 0o755)
	if err := replaceExecutable(exe, bin); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new version" {
		t.Fatalf("exe now contains %q", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal("temporary file left behind")
	}
}
