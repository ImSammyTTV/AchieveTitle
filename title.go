package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Twitch rejects titles longer than 140 characters, counted in UTF-16 units
// (so most emoji count as 2).
const maxTitleLen = 140

func titleLen(s string) int { return len(utf16.Encode([]rune(s))) }

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

// TemplateVars documents the placeholders shown in the settings page.
var TemplateVars = []string{"custom", "game", "unlocked", "total", "percent", "remaining", "latest", "latest_rarity", "next", "next_rarity"}

func buildVars(cfg Config, game string, p *Progress) map[string]string {
	v := map[string]string{"custom": cfg.CustomTitle, "game": game}
	if p == nil {
		return v
	}
	v["unlocked"] = fmt.Sprint(p.Unlocked)
	v["total"] = fmt.Sprint(p.Total)
	v["remaining"] = fmt.Sprint(p.Total - p.Unlocked)
	v["percent"] = fmt.Sprintf("%d%%", p.Unlocked*100/p.Total)
	v["latest"] = "none yet"
	if p.Latest != nil {
		v["latest"] = p.Latest.Name
		v["latest_rarity"] = rarity(p.Latest.Percent)
	}
	if p.RarestLocked != nil {
		v["next"] = p.RarestLocked.Name
		v["next_rarity"] = rarity(p.RarestLocked.Percent)
	}
	return v
}

func rarity(pct float64) string {
	if pct <= 0 {
		return ""
	}
	if pct < 1 {
		return fmt.Sprintf("%.1f%%", pct)
	}
	return fmt.Sprintf("%.0f%%", pct)
}

// renderTitle fills the template and fits it to Twitch's length limit, shortening
// the achievement names first so the counter and custom text survive.
func renderTitle(tmpl string, vars map[string]string) string {
	fill := func(v map[string]string) string {
		out := placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
			return v[m[1:len(m)-1]]
		})
		return strings.Join(strings.Fields(out), " ")
	}
	out := fill(vars)
	for _, k := range []string{"next", "latest", "game"} {
		for titleLen(out) > maxTitleLen && utf8.RuneCountInString(vars[k]) > 8 {
			r := []rune(strings.TrimSuffix(vars[k], "…"))
			vars[k] = string(r[:len(r)-1]) + "…"
			out = fill(vars)
		}
	}
	for titleLen(out) > maxTitleLen {
		r := []rune(strings.TrimSuffix(out, "…"))
		out = string(r[:len(r)-1]) + "…"
	}
	return out
}
