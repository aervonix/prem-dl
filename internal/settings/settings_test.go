package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeSettings(t *testing.T, dir string, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOpenKeepsValidFieldsWhenOneIsInvalid(t *testing.T) {
	dir := t.TempDir()
	s := Default()
	s.DownloadDir = `D:\Videos`
	s.MaxConcurrent = 20 // out of range, e.g. hand-edited
	s.Cookies = Cookies{Mode: "browser", Browser: "firefox", Profile: "work"}
	s.Proxy = "socks5://127.0.0.1:1080"
	s.Defaults.Preset = "custom"
	s.Defaults.FormatID = "137+140"
	s.Defaults.SectionStart = "not a time"
	raw := writeSettings(t, dir, s)

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Get()
	if got.MaxConcurrent != Default().MaxConcurrent {
		t.Errorf("invalid MaxConcurrent should reset to the default, got %d", got.MaxConcurrent)
	}
	if got.DownloadDir != `D:\Videos` || got.Cookies != s.Cookies || got.Proxy != s.Proxy {
		t.Errorf("valid fields were lost: %+v", got)
	}
	if got.Defaults.Preset != "custom" || got.Defaults.FormatID != "137+140" {
		t.Errorf("custom preset needs its format to survive the repair: %q %q", got.Defaults.Preset, got.Defaults.FormatID)
	}
	if got.Defaults.SectionStart != "" {
		t.Errorf("invalid section start should be cleared, got %q", got.Defaults.SectionStart)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("repaired settings must validate: %v", err)
	}
	backup, err := os.ReadFile(filepath.Join(dir, "settings.json.invalid"))
	if err != nil || string(backup) != string(raw) {
		t.Errorf("the original file should be backed up before it can be overwritten: %v", err)
	}
}

func TestOpenCorruptFileFallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Get().MaxConcurrent != Default().MaxConcurrent {
		t.Error("expected defaults")
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json.corrupt")); err != nil {
		t.Errorf("corrupt file should be kept: %v", err)
	}
}

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Update(func(s *Settings) { s.Theme = "dark"; s.DownloadDir = `E:\dl` }); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if g := again.Get(); g.Theme != "dark" || g.DownloadDir != `E:\dl` {
		t.Errorf("reloaded %+v", g)
	}
}

func TestAudioLangIsALanguageCode(t *testing.T) {
	for lang, ok := range map[string]bool{"": true, "de": true, " en-US ": true, "zh-Hans": true, "es-419": true, "d]e": false, "en,de": false, "--exec": false} {
		o := Default().Defaults
		o.AudioLang = lang
		if err := ValidateOptions(&o); (err == nil) != ok {
			t.Errorf("%q: err = %v", lang, err)
		}
	}
}
