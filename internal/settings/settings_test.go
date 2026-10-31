package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestPresetsAreNormalised(t *testing.T) {
	s := Default()
	music := s.Presets[0].Options
	music.Preset, music.FormatID = "custom", "251"
	music.SectionStart, music.IgnoreArchive, music.WaitForVideo, music.AudioLang = "1:00", true, true, "de"
	s.Presets = []NamedPreset{{Name: "  Music ", Sites: []string{"https://www.Music.YouTube.com/watch?v=x", "", "music.youtube.com"}, Options: music}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	p := s.Presets[0]
	if p.Name != "Music" || len(p.Sites) != 1 || p.Sites[0] != "music.youtube.com" {
		t.Errorf("name and sites not cleaned: %+v", p)
	}
	if o := p.Options; o.Preset != "best" || o.FormatID != "" || o.SectionStart != "" || o.IgnoreArchive || o.WaitForVideo || o.AudioLang != "" {
		t.Errorf("choices tied to one video must not be kept in a preset: %+v", o)
	}

	s.Presets = nil
	if err := s.Validate(); err != nil || s.Presets == nil {
		t.Errorf("no presets must be an empty list, not null: %v", err)
	}
}

func TestPresetProblems(t *testing.T) {
	base := StarterPresets()
	for name, edit := range map[string]func(ps []NamedPreset) []NamedPreset{
		"no name":     func(ps []NamedPreset) []NamedPreset { ps[0].Name = " "; return ps },
		"same name":   func(ps []NamedPreset) []NamedPreset { ps[1].Name = "music"; return ps },
		"shared site": func(ps []NamedPreset) []NamedPreset { ps[1].Sites = []string{"music.youtube.com"}; return ps },
		"not a site":  func(ps []NamedPreset) []NamedPreset { ps[1].Sites = []string{"my site"}; return ps },
		"bare word":   func(ps []NamedPreset) []NamedPreset { ps[1].Sites = []string{"localhost"}; return ps },
		"bad options": func(ps []NamedPreset) []NamedPreset { ps[1].Options.Subtitles.Langs = nil; return ps },
		"too many":    func(ps []NamedPreset) []NamedPreset { return make([]NamedPreset, MaxPresets+1) },
		"long name":   func(ps []NamedPreset) []NamedPreset { ps[0].Name = strings.Repeat("x", 41); return ps },
	} {
		s := Default()
		s.Presets = edit(clone(Settings{Presets: base}).Presets)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCleanSite(t *testing.T) {
	for in, want := range map[string]string{
		"music.youtube.com":                   "music.youtube.com",
		" HTTPS://www.YouTube.com/@x/videos ": "youtube.com",
		"vimeo.com:443":                       "vimeo.com",
		"example.org.":                        "example.org",
		"":                                    "",
	} {
		if got := CleanSite(in); got != want {
			t.Errorf("CleanSite(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOldSettingsGetStarterPresets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"downloadDir":"D:\dl","theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Get().Presets; len(got) != 2 || got[0].Name != "Music" {
		t.Errorf("a file from before presets should start with the starters, got %+v", got)
	}

	// Deleting them all sticks.
	if _, err := st.Update(func(s *Settings) { s.Presets = nil }); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Get().Presets; len(got) != 0 {
		t.Errorf("deleted presets came back: %+v", got)
	}
}

// A saved preset missing a field that was added later gets that field's zero value, not the
// value of the starter preset that happens to sit in the same position.
func TestSavedPresetsDontInheritStarterFields(t *testing.T) {
	dir := t.TempDir()
	raw := `{"presets":[{"name":"Mine","options":{"preset":"720","container":"mp4"}}]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Get().Presets
	if len(got) != 1 || got[0].Name != "Mine" {
		t.Fatalf("presets %+v", got)
	}
	if starter := StarterPresets()[0].Options; starter.EmbedThumbnail && got[0].Options.EmbedThumbnail {
		t.Errorf("Mine took the Music preset's thumbnail choice: %+v", got[0].Options)
	}
}

func TestRepairKeepsTheGoodPresets(t *testing.T) {
	dir := t.TempDir()
	s := Default()
	s.Presets = append(s.Presets, NamedPreset{Name: "Broken", Sites: []string{"not a site"}, Options: s.Defaults})
	writeSettings(t, dir, s)
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Get().Presets
	if len(got) != 2 || got[0].Name != "Music" || got[1].Name != "Archive" {
		t.Errorf("only the broken preset should go, got %+v", got)
	}
}

func TestChapterOptions(t *testing.T) {
	o := Default().Defaults
	o.Chapters, o.SplitChapters = []string{"Intro", "What are neurons?"}, true
	if err := ValidateOptions(&o); err != nil {
		t.Fatal(err)
	}
	if o.SplitChapters {
		t.Error("picked chapters are saved one file each already; the split must be off")
	}
	o.SectionStart = "1:00"
	if err := ValidateOptions(&o); err == nil {
		t.Error("chapters and a time range together must be refused")
	}
	o.SectionStart, o.Chapters = "", []string{" "}
	if err := ValidateOptions(&o); err == nil {
		t.Error("a blank chapter title must be refused")
	}
	o.Chapters = nil
	if err := ValidateOptions(&o); err != nil || o.Chapters == nil {
		t.Errorf("no chapters must be an empty list, not null: %v", err)
	}

	s := Default()
	s.Presets[0].Options.Chapters = []string{"Intro"}
	if err := s.Validate(); err != nil || len(s.Presets[0].Options.Chapters) != 0 {
		t.Errorf("presets must not keep chapters: %v %v", err, s.Presets[0].Options.Chapters)
	}
}

func TestPresetFor(t *testing.T) {
	ps := []NamedPreset{{Name: "Tube", Sites: []string{"youtube.com"}}, {Name: "Music", Sites: []string{"music.youtube.com"}}}
	for link, want := range map[string]string{
		"https://music.youtube.com/watch?v=x": "Music",
		"https://www.youtube.com/watch?v=x":   "Tube",
		"https://youtube.com./watch?v=x":      "Tube",
		"https://notyoutube.com/x":            "",
		"https://vimeo.com/1":                 "",
	} {
		got := ""
		if p := PresetFor(link, ps); p != nil {
			got = p.Name
		}
		if got != want {
			t.Errorf("%s: %q, want %q", link, got, want)
		}
	}
	if p := FindPreset(" music ", ps); p == nil || p.Name != "Music" {
		t.Errorf("FindPreset: %+v", p)
	}
}

func TestOptionTemplateChecked(t *testing.T) {
	o := Default().Defaults
	for _, bad := range []string{"%(title)s", "-o x.%(ext)s"} {
		o.FilenameTemplate = bad
		if ValidateOptions(&o) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	o.FilenameTemplate = "  %(title)s [%(id)s].%(ext)s "
	if err := ValidateOptions(&o); err != nil || o.FilenameTemplate != "%(title)s [%(id)s].%(ext)s" {
		t.Errorf("%q, %v", o.FilenameTemplate, err)
	}
}

func TestActiveHoursOpen(t *testing.T) {
	at := func(hm string) time.Time {
		tm, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-31 "+hm, time.Local)
		return tm
	}
	night := ActiveHours{On: true, From: "23:00", To: "07:00"}
	for hm, want := range map[string]bool{"23:30": true, "02:00": true, "06:59": true, "07:00": false, "12:00": false, "22:59": false} {
		if open, _ := night.Open(at(hm)); open != want {
			t.Errorf("night at %s: %v", hm, open)
		}
	}
	if _, opens := night.Open(at("12:00")); !opens.Equal(at("23:00")) {
		t.Errorf("opens %v", opens)
	}
	day := ActiveHours{On: true, From: "09:00", To: "17:00"}
	if _, opens := day.Open(at("18:00")); !opens.Equal(at("09:00").AddDate(0, 0, 1)) {
		t.Errorf("after hours opens %v, want tomorrow 09:00", opens)
	}
	if open, _ := (ActiveHours{From: "09:00", To: "17:00"}).Open(at("18:00")); !open {
		t.Error("off means always")
	}
	s := Default()
	s.ActiveHours = ActiveHours{On: true, From: "25:00", To: "07:00"}
	if s.Validate() == nil {
		t.Error("25:00 accepted")
	}
}

func TestOptionTemplateStaysInTheFolder(t *testing.T) {
	for _, bad := range []string{"/tmp/%(title)s.%(ext)s", `C:\x\%(title)s.%(ext)s`, `\\host\s\%(title)s.%(ext)s`, "../%(title)s.%(ext)s", `a\..\%(title)s.%(ext)s`} {
		o := Default().Defaults
		o.FilenameTemplate = bad
		if ValidateOptions(&o) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	o := Default().Defaults
	o.FilenameTemplate = "%(channel)s/%(title)s..%(ext)s"
	if err := ValidateOptions(&o); err != nil {
		t.Errorf("a subfolder: %v", err)
	}
}
