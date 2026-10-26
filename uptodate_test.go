package main

import (
	"testing"

	"yt-dlp-gui/internal/settings"
)

func TestUpToDate(t *testing.T) {
	store, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: store}
	set := func(channel, latest, latestChannel string) {
		t.Helper()
		if _, err := store.Update(func(s *settings.Settings) {
			s.UpdateChannel, s.LatestYtdlp, s.LatestYtdlpChannel = channel, latest, latestChannel
		}); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name                           string
		channel, latest, latestChannel string
		installed                      string
		want                           bool
	}{
		{"never checked", "stable", "", "", "2026.10.21", false},
		{"same as latest", "stable", "2026.10.21", "stable", "2026.10.21", true},
		{"newer release out", "stable", "2026.10.25", "stable", "2026.10.21", false},
		{"checked on another channel", "nightly", "2026.10.21", "stable", "2026.10.21", false},
		{"nightly build after the stable release", "nightly", "2026.10.21", "nightly", "2026.10.25.232749", true},
		{"nightly installed, back on stable", "stable", "2026.10.21", "stable", "2026.10.25.232749", false},
		{"yt-dlp not installed", "stable", "2026.10.21", "stable", "", false},
	}
	for _, c := range cases {
		set(c.channel, c.latest, c.latestChannel)
		if got := a.upToDate(c.installed); got != c.want {
			t.Errorf("%s: upToDate(%q) = %v, want %v", c.name, c.installed, got, c.want)
		}
	}
}
