package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVerifySteamLogin(t *testing.T) {
	valid := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q, _ := url.ParseQuery(string(b))
		if q.Get("openid.mode") != "check_authentication" || q.Get("openid.sig") != "sig" {
			t.Errorf("bad verification request: %v", q)
		}
		if valid {
			io.WriteString(w, "ns:http://specs.openid.net/auth/2.0\nis_valid:true\n")
		} else {
			io.WriteString(w, "ns:http://specs.openid.net/auth/2.0\nis_valid:false\n")
		}
	}))
	defer srv.Close()
	steamOpenID = srv.URL

	returnTo := "http://localhost:7878/auth/steam?n=abc"
	resp := url.Values{
		"openid.mode":       {"id_res"},
		"openid.return_to":  {returnTo},
		"openid.claimed_id": {"https://steamcommunity.com/openid/id/76561198035066303"},
		"openid.sig":        {"sig"},
	}
	s := newSteam()
	id, err := s.VerifySteamLogin(context.Background(), resp, returnTo)
	if err != nil || id != "76561198035066303" {
		t.Fatalf("got %q, %v", id, err)
	}

	valid = false
	if _, err := s.VerifySteamLogin(context.Background(), resp, returnTo); err == nil {
		t.Fatal("a response Steam doesn't confirm must be rejected")
	}
	valid = true
	if _, err := s.VerifySteamLogin(context.Background(), resp, "http://localhost:7878/auth/steam?n=other"); err == nil {
		t.Fatal("a response for another sign-in must be rejected")
	}
	cancelled := url.Values{"openid.mode": {"cancel"}}
	if _, err := s.VerifySteamLogin(context.Background(), cancelled, returnTo); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancelled, got %v", err)
	}
}

func TestSteamLoginURL(t *testing.T) {
	u, _ := url.Parse(steamLoginURL("http://localhost:7878", "http://localhost:7878/auth/steam?n=abc"))
	q := u.Query()
	if q.Get("openid.return_to") != "http://localhost:7878/auth/steam?n=abc" || q.Get("openid.mode") != "checkid_setup" {
		t.Fatalf("bad login URL: %s", u)
	}
}
