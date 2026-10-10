package main

import (
	"strings"
	"testing"
)

func TestRenderTitle(t *testing.T) {
	cfg := defaultConfig()
	p := &Progress{Unlocked: 38, Total: 50, Latest: &Achievement{Name: "Dragon Slayer", Percent: 4.2}}
	got := renderTitle(cfg.Template, buildVars(cfg, "Skyrim", p))
	want := "Chill stream [38/50 Achievements] 🏆 Dragon Slayer"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderTitleFitsTwitchLimit(t *testing.T) {
	cfg := defaultConfig()
	cfg.CustomTitle = strings.Repeat("x", 90)
	p := &Progress{Unlocked: 1, Total: 2, Latest: &Achievement{Name: strings.Repeat("Very Long Achievement ", 5)}}
	got := renderTitle(cfg.Template, buildVars(cfg, "", p))
	if n := titleLen(got); n > maxTitleLen {
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

func TestTitleLenCountsEmojiLikeTwitch(t *testing.T) {
	if n := titleLen("Potato potata [45/179 Achievements] 🏆 Hoarder"); n != 46 {
		t.Fatalf("got %d, Twitch shows 46", n)
	}
}

func TestCleanTags(t *testing.T) {
	got := CleanTags([]string{"chill stream", "Achievement-Hunting", "chillstream", "", "abcdefghijklmnopqrstuvwxyz0"})
	want := []string{"chillstream", "AchievementHunting", "abcdefghijklmnopqrstuvwxy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeName(t *testing.T) {
	if normalizeName("Brotato™") != normalizeName("brotato") {
		t.Fatal("trademark sign should be ignored")
	}
}

func TestRainbowBar(t *testing.T) {
	st := BarStyle{Filled: "🟥🟧🟨🟩🟦🟪", Empty: "⬛", Length: 6}
	if got := progressBar(6, 6, st); got != "🟥🟧🟨🟩🟦🟪" {
		t.Errorf("full bar = %q", got)
	}
	if got := progressBar(3, 6, st); got != "🟥🟧🟨⬛⬛⬛" {
		t.Errorf("half bar = %q", got)
	}
	if got := progressBar(10, 10, BarStyle{Filled: "❤️🧡", Empty: "🖤", Length: 5}); got != "❤️🧡❤️🧡❤️" {
		t.Errorf("hearts = %q", got)
	}
}
