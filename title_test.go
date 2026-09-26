package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderTitle(t *testing.T) {
	cfg := defaultConfig()
	p := &Progress{Unlocked: 38, Total: 50, Latest: &Achievement{Name: "Dragon Slayer", Percent: 4.2}}
	got := renderTitle(cfg.Template, buildVars(cfg, "Skyrim", p))
	want := "Chill stream [38/50 Achievements] Last: Dragon Slayer"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderTitleFitsTwitchLimit(t *testing.T) {
	cfg := defaultConfig()
	cfg.CustomTitle = strings.Repeat("x", 90)
	p := &Progress{Unlocked: 1, Total: 2, Latest: &Achievement{Name: strings.Repeat("Very Long Achievement ", 5)}}
	got := renderTitle(cfg.Template, buildVars(cfg, "", p))
	if n := utf8.RuneCountInString(got); n > maxTitleLen {
		t.Fatalf("title is %d chars", n)
	}
	if !strings.Contains(got, "[1/2 Achievements]") || !strings.Contains(got, "…") {
		t.Fatalf("counter should survive and name be shortened: %q", got)
	}
}

func TestFallbackWithoutProgress(t *testing.T) {
	cfg := defaultConfig()
	if got := renderTitle(cfg.FallbackTmpl, buildVars(cfg, "", nil)); got != "Chill stream" {
		t.Fatalf("got %q", got)
	}
}
