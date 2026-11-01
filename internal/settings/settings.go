// Package settings defines the user's preferences and per-download options, and persists them.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ConfigDirName is the folder created under the user's config directory.
const ConfigDirName = "yt-dlp-gui"

const DefaultTemplate = "%(title)s.%(ext)s"

// SubtitleOptions controls subtitle handling. Mode is none | files | embed | both.
type SubtitleOptions struct {
	Mode   string   `json:"mode"`
	Langs  []string `json:"langs"`
	Auto   bool     `json:"auto"`   // include auto-generated captions
	Format string   `json:"format"` // "" (keep original) | srt | vtt | ass
}

// DownloadOptions are chosen per download in the UI; Settings.Defaults seeds them.
type DownloadOptions struct {
	// Preset is best | 2160 | 1440 | 1080 | 720 | 480 | 360 | audio | custom.
	Preset   string `json:"preset"`
	FormatID string `json:"formatId"` // yt-dlp -f expression, used when Preset == custom
	// FormatAudioOnly: the custom format is audio only. It is saved as an audio file in its own
	// codec (.opus, .m4a), which can hold cover art, where WebM can't.
	FormatAudioOnly bool   `json:"formatAudioOnly"`
	Container       string `json:"container"` // "" | mp4 | mkv | webm
	// Compatible prefers H.264 video and AAC audio, which open in editors, TVs and older devices
	// that refuse VP9, AV1 or Opus. On YouTube that means 1080p at most. Video presets only.
	Compatible   bool   `json:"compatible"`
	AudioFormat  string `json:"audioFormat"`  // best | mp3 | m4a | opus | flac | wav | aac
	AudioQuality string `json:"audioQuality"` // "0" (best) .. "10" (worst)

	Subtitles SubtitleOptions `json:"subtitles"`

	EmbedMetadata  bool `json:"embedMetadata"`
	EmbedThumbnail bool `json:"embedThumbnail"`
	// SquareCover crops an audio download's cover art to a square: YouTube's thumbnails are
	// 16:9, with the album art letterboxed inside.
	SquareCover      bool `json:"squareCover"`
	EmbedChapters    bool `json:"embedChapters"`
	WriteDescription bool `json:"writeDescription"`
	WriteInfoJSON    bool `json:"writeInfoJson"`
	WriteThumbnail   bool `json:"writeThumbnail"`

	SponsorBlock []string `json:"sponsorBlock"` // categories to cut out

	SectionStart string `json:"sectionStart"` // e.g. 1:30 — empty means from the start
	SectionEnd   string `json:"sectionEnd"`   // empty means until the end
	// Chapters downloads only these chapters (by title), each as its own file. Not with a
	// time range.
	Chapters []string `json:"chapters"`

	OutputDir string `json:"outputDir"` // "" uses Settings.DownloadDir
	// FilenameTemplate names the files instead of Settings.FilenameTemplate (a preset for
	// podcasts dated, one for archives with the ID); "" uses that one.
	FilenameTemplate  string `json:"filenameTemplate"`
	PlaylistFolder    bool   `json:"playlistFolder"`
	PlaylistNumbering bool   `json:"playlistNumbering"`
	// PlaylistFile writes "<Playlist>.m3u8" beside a playlist's videos, in playlist order.
	PlaylistFile bool `json:"playlistFile"`
	// PlaylistTags tags audio from a playlist with the playlist as its album and its position
	// as the track number, so music players keep it together and in order.
	PlaylistTags bool `json:"playlistTags"`
	// IgnoreArchive downloads the video even when the download archive lists it.
	IgnoreArchive bool `json:"ignoreArchive"`
	// SponsorBlockMark marks the SponsorBlock segments as chapters instead of cutting them out.
	SponsorBlockMark bool `json:"sponsorBlockMark"`
	// SplitChapters also saves each chapter as its own file, in a folder named after the video.
	SplitChapters bool `json:"splitChapters"`
	// LiveFromStart records a live stream from its beginning rather than from now.
	LiveFromStart bool `json:"liveFromStart"`
	// WaitForVideo waits for a scheduled stream or premiere to start, then downloads it.
	WaitForVideo bool `json:"waitForVideo"`
	// AudioLang picks the audio track by language ("en", "es-419") on dubbed videos; "" takes
	// the default. Formats without it fall back to the default track.
	AudioLang string `json:"audioLang"`
}

// NamedPreset is a saved set of download options the Download page offers by name. A link to
// one of its Sites (music.youtube.com) picks it automatically.
type NamedPreset struct {
	Name    string          `json:"name"`
	Sites   []string        `json:"sites"` // host names; each also covers its subdomains
	Options DownloadOptions `json:"options"`
}

const maxChapters = 500

// MaxPresets bounds Settings.Presets.
const MaxPresets = 50

// Cookies describes where yt-dlp should read login cookies from.
type Cookies struct {
	Mode    string `json:"mode"`    // none | browser | file
	Browser string `json:"browser"` // chrome | firefox | edge | ...
	Profile string `json:"profile"` // optional browser profile name
	File    string `json:"file"`    // Netscape-format cookies.txt
}

// Settings is everything persisted in settings.json.
type Settings struct {
	DownloadDir         string          `json:"downloadDir"`
	FilenameTemplate    string          `json:"filenameTemplate"`
	MaxConcurrent       int             `json:"maxConcurrent"`
	ConcurrentFragments int             `json:"concurrentFragments"`
	RateLimit           string          `json:"rateLimit"` // e.g. 2M, 500K; empty = unlimited
	Proxy               string          `json:"proxy"`
	ForceIPv4           bool            `json:"forceIPv4"` // for networks that hand out IPv6 addresses that don't work
	Retries             int             `json:"retries"`
	AutoRetry           bool            `json:"autoRetry"`     // retry downloads that failed with a passing problem
	SleepRequests       float64         `json:"sleepRequests"` // seconds between requests while a download reads the site
	SleepInterval       float64         `json:"sleepInterval"` // seconds to wait before each download starts
	UseArchive          bool            `json:"useArchive"`    // skip videos already downloaded before
	Cookies             Cookies         `json:"cookies"`
	HelperBrowser       string          `json:"helperBrowser"`    // the browser the Chrome helper runs in; "" = the default one
	CheckAppUpdates     bool            `json:"checkAppUpdates"`  // look for new Prem-dl releases
	Theme               string          `json:"theme"`            // system | light | dark
	NotifySound         bool            `json:"notifySound"`      // a click when a download finishes
	NotifyPopup         bool            `json:"notifyPopup"`      // a card in the corner when one finishes
	NotifySystem        bool            `json:"notifySystem"`     // a Windows notification when one finishes while the app is in the background
	CloseToTray         bool            `json:"closeToTray"`      // closing the window keeps the app running in the tray
	StartWithWindows    bool            `json:"startWithWindows"` // start in the tray when the user signs in
	KeepAwake           bool            `json:"keepAwake"`        // the computer doesn't sleep while downloads run
	ActiveHours         ActiveHours     `json:"activeHours"`      // when downloads may start
	FollowEvery         int             `json:"followEvery"`      // hours between checks of followed channels; 0 = only when asked
	ExtraArgs           string          `json:"extraArgs"`
	Defaults            DownloadOptions `json:"defaults"`
	Presets             []NamedPreset   `json:"presets"`
}

// Default returns first-run settings.
func Default() Settings {
	return Settings{
		DownloadDir:         DownloadsFolder(),
		FilenameTemplate:    DefaultTemplate,
		MaxConcurrent:       2,
		ConcurrentFragments: 4,
		Retries:             10,
		AutoRetry:           true,
		UseArchive:          false,
		Cookies:             Cookies{Mode: "none", Browser: "firefox"},
		CheckAppUpdates:     true,
		Theme:               "system",
		NotifySound:         true,
		NotifyPopup:         true,
		NotifySystem:        true,
		KeepAwake:           true,
		ActiveHours:         ActiveHours{From: "01:00", To: "07:00"},
		FollowEvery:         6,
		Defaults: DownloadOptions{
			Preset:            "best",
			AudioFormat:       "mp3",
			AudioQuality:      "0",
			Subtitles:         SubtitleOptions{Mode: "none", Langs: []string{"en"}},
			EmbedMetadata:     true,
			EmbedThumbnail:    false,
			EmbedChapters:     true,
			PlaylistFolder:    true,
			PlaylistNumbering: true,
		},
		Presets: StarterPresets(),
	}
}

// StarterPresets are the presets a new install (or one from before presets) begins with.
func StarterPresets() []NamedPreset {
	return []NamedPreset{
		{
			Name:  "Music",
			Sites: []string{"music.youtube.com"},
			Options: DownloadOptions{
				Preset:            "audio",
				AudioFormat:       "mp3",
				AudioQuality:      "0",
				Subtitles:         SubtitleOptions{Mode: "none", Langs: []string{"en"}},
				EmbedMetadata:     true,
				EmbedThumbnail:    true,
				SquareCover:       true,
				EmbedChapters:     true,
				PlaylistFolder:    true,
				PlaylistNumbering: true,
				PlaylistFile:      true,
				PlaylistTags:      true,
			},
		},
		{
			Name:  "Archive",
			Sites: []string{},
			Options: DownloadOptions{
				Preset:            "best",
				Container:         "mkv",
				AudioFormat:       "mp3",
				AudioQuality:      "0",
				Subtitles:         SubtitleOptions{Mode: "both", Langs: []string{"en"}},
				EmbedMetadata:     true,
				EmbedThumbnail:    true,
				EmbedChapters:     true,
				WriteDescription:  true,
				WriteInfoJSON:     true,
				WriteThumbnail:    true,
				PlaylistFolder:    true,
				PlaylistNumbering: true,
			},
		},
	}
}

// FollowIntervals are the choices for Settings.FollowEvery, in hours.
var FollowIntervals = map[int]bool{0: true, 1: true, 3: true, 6: true, 12: true, 24: true}

var (
	rateLimitRe = regexp.MustCompile(`^\d+(\.\d+)?[KkMmGg]?$`)
	timeRe      = regexp.MustCompile(`^(\d+:){0,2}\d+(\.\d+)?$`)
	audioLangRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$`)
	siteRe      = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	langRe      = regexp.MustCompile(`^[A-Za-z0-9_.*\-\[\]()|?+^$]+$`)
	sponsorCats = map[string]bool{
		"sponsor": true, "intro": true, "outro": true, "selfpromo": true, "preview": true,
		"filler": true, "interaction": true, "music_offtopic": true,
		// poi_highlight and chapter are SponsorBlock categories too, but --sponsorblock-remove
		// refuses them and every download would fail.
	}
	oneOf = func(v string, allowed ...string) bool {
		for _, a := range allowed {
			if v == a {
				return true
			}
		}
		return false
	}
)

// driveRe matches a Windows drive at the start of a path (C:\ or C:/): a preset file can come
// from the other system.
var driveRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// checkTemplate refuses a file name template that would lose the extension or read as a flag.
func checkTemplate(t string) error {
	if !strings.Contains(t, "%(ext)s") {
		return errors.New("the filename template must end with %(ext)s so files keep their extension")
	}
	if strings.HasPrefix(t, "-") {
		return errors.New("the filename template can't start with a dash")
	}
	return nil
}

// ValidateOptions normalises the options in place and rejects values that would make
// yt-dlp misbehave. The returned error message is shown to the user.
func ValidateOptions(o *DownloadOptions) error {
	o.FilenameTemplate = strings.TrimSpace(o.FilenameTemplate)
	if o.FilenameTemplate != "" {
		if err := checkTemplate(o.FilenameTemplate); err != nil {
			return err
		}
		// Options travel (presets, exported follows), so their template stays inside the
		// download folder: Settings' own template is the place for anything else.
		if filepath.IsAbs(o.FilenameTemplate) || strings.HasPrefix(o.FilenameTemplate, "/") || strings.HasPrefix(o.FilenameTemplate, `\`) || driveRe.MatchString(o.FilenameTemplate) ||
			slices.Contains(strings.FieldsFunc(o.FilenameTemplate, func(r rune) bool { return r == '/' || r == '\\' }), "..") {
			return errors.New("a preset's file name template stays inside the download folder: no full paths or ..")
		}
	}
	if !oneOf(o.Preset, "best", "2160", "1440", "1080", "720", "480", "360", "audio", "custom") {
		o.Preset = "best"
	}
	o.FormatID = strings.TrimSpace(o.FormatID)
	if o.Preset == "custom" && o.FormatID == "" {
		return errors.New("choose a format, or switch to a quality preset")
	}
	if o.Preset != "custom" {
		o.FormatAudioOnly = false
	}
	if !oneOf(o.Container, "", "mp4", "mkv", "webm") {
		o.Container = ""
	}
	if !oneOf(o.AudioFormat, "best", "mp3", "m4a", "opus", "flac", "wav", "aac") {
		o.AudioFormat = "mp3"
	}
	if q := o.AudioQuality; q != "" {
		if len(q) > 5 || strings.ContainsAny(q, " -") {
			o.AudioQuality = "0"
		}
	}

	s := &o.Subtitles
	if !oneOf(s.Mode, "none", "files", "embed", "both") {
		s.Mode = "none"
	}
	if !oneOf(s.Format, "", "srt", "vtt", "ass") {
		s.Format = ""
	}
	clean := make([]string, 0, len(s.Langs))
	for _, l := range s.Langs {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if !langRe.MatchString(l) || strings.HasPrefix(l, "-") {
			return fmt.Errorf("%q is not a valid subtitle language code", l)
		}
		clean = append(clean, l)
	}
	s.Langs = clean
	if s.Mode != "none" && len(s.Langs) == 0 {
		return errors.New("pick at least one subtitle language")
	}

	cats := make([]string, 0, len(o.SponsorBlock))
	for _, c := range o.SponsorBlock {
		if !sponsorCats[c] {
			return fmt.Errorf("unknown SponsorBlock category %q", c)
		}
		cats = append(cats, c)
	}
	o.SponsorBlock = cats

	o.SectionStart = strings.TrimSpace(o.SectionStart)
	o.SectionEnd = strings.TrimSpace(o.SectionEnd)
	if o.SectionStart != "" && !timeRe.MatchString(o.SectionStart) {
		return fmt.Errorf("start time %q must look like 90, 1:30 or 1:02:03", o.SectionStart)
	}
	if o.SectionEnd != "" && !timeRe.MatchString(o.SectionEnd) {
		return fmt.Errorf("end time %q must look like 90, 1:30 or 1:02:03", o.SectionEnd)
	}
	if o.Chapters == nil {
		o.Chapters = []string{}
	}
	if len(o.Chapters) > 0 {
		if o.SectionStart != "" || o.SectionEnd != "" {
			return errors.New("choose chapters or a time range, not both")
		}
		if len(o.Chapters) > maxChapters {
			return fmt.Errorf("at most %d chapters can be picked", maxChapters)
		}
		for _, c := range o.Chapters {
			if strings.TrimSpace(c) == "" || len(c) > 500 {
				return errors.New("a picked chapter has no usable title")
			}
		}
		// Each chapter is saved as its own file already.
		o.SplitChapters = false
	}
	o.AudioLang = strings.TrimSpace(o.AudioLang)
	if o.AudioLang != "" && !audioLangRe.MatchString(o.AudioLang) {
		return fmt.Errorf("%q is not a language code", o.AudioLang)
	}
	// yt-dlp resolves a relative folder against the app's working directory.
	o.OutputDir = strings.TrimSpace(o.OutputDir)
	if o.OutputDir != "" && !filepath.IsAbs(o.OutputDir) {
		return fmt.Errorf("the Save to folder %q must be a full path, like C:\\Videos", o.OutputDir)
	}
	return nil
}

// Validate normalises s in place and reports the first problem found.
func (s *Settings) Validate() error {
	def := Default()
	s.DownloadDir = strings.TrimSpace(s.DownloadDir)
	if s.DownloadDir == "" {
		return errors.New("choose a download folder")
	}
	s.FilenameTemplate = strings.TrimSpace(s.FilenameTemplate)
	if s.FilenameTemplate == "" {
		s.FilenameTemplate = def.FilenameTemplate
	}
	if err := checkTemplate(s.FilenameTemplate); err != nil {
		return err
	}
	if err := s.ActiveHours.validate(); err != nil {
		return err
	}
	if s.MaxConcurrent < 1 || s.MaxConcurrent > 10 {
		return errors.New("simultaneous downloads must be between 1 and 10")
	}
	if s.ConcurrentFragments < 1 || s.ConcurrentFragments > 16 {
		return errors.New("fragment threads must be between 1 and 16")
	}
	if s.Retries < 0 || s.Retries > 50 {
		return errors.New("retries must be between 0 and 50")
	}
	if s.SleepRequests < 0 || s.SleepRequests > 300 || s.SleepInterval < 0 || s.SleepInterval > 3600 {
		return errors.New("pauses must be between 0 and 300 seconds between requests, and 0 and 3600 before a download")
	}
	s.RateLimit = strings.TrimSpace(s.RateLimit)
	if s.RateLimit != "" && !rateLimitRe.MatchString(s.RateLimit) {
		return errors.New("speed limit must look like 500K or 2M")
	}
	s.Proxy = strings.TrimSpace(s.Proxy)
	if s.Proxy != "" && !strings.Contains(s.Proxy, "://") {
		return errors.New("proxy must include a scheme, e.g. http://127.0.0.1:8080 or socks5://127.0.0.1:1080")
	}
	if !oneOf(s.Cookies.Mode, "none", "browser", "file") {
		s.Cookies.Mode = "none"
	}
	if !oneOf(s.Cookies.Browser, "brave", "chrome", "chromium", "edge", "firefox", "opera", "safari", "vivaldi", "whale") {
		s.Cookies.Browser = def.Cookies.Browser
	}
	s.Cookies.Profile = strings.TrimSpace(s.Cookies.Profile)
	if strings.ContainsAny(s.Cookies.Profile, ":\n\r") {
		return errors.New("browser profile name can't contain a colon")
	}
	if len(s.HelperBrowser) > 100 || strings.Trim(s.HelperBrowser, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.:_-") != "" {
		s.HelperBrowser = ""
	}
	if s.Cookies.Mode == "file" && strings.TrimSpace(s.Cookies.File) == "" {
		return errors.New("choose a cookies.txt file, or switch the cookie source")
	}
	if !oneOf(s.Theme, "system", "light", "dark") {
		s.Theme = "system"
	}
	if !FollowIntervals[s.FollowEvery] {
		return errors.New("choose how often to check followed channels from the list")
	}
	if err := ValidateOptions(&s.Defaults); err != nil {
		return err
	}
	return validatePresets(&s.Presets)
}

// validatePresets normalises the presets in place: names are trimmed and unique, sites are bare
// host names that belong to one preset each, and the options carry nothing tied to one video.
func validatePresets(ps *[]NamedPreset) error {
	if *ps == nil {
		*ps = []NamedPreset{}
	}
	if len(*ps) > MaxPresets {
		return fmt.Errorf("you can keep up to %d presets", MaxPresets)
	}
	names := map[string]bool{}
	owner := map[string]string{}
	for i := range *ps {
		p := &(*ps)[i]
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			return errors.New("every preset needs a name")
		}
		if len([]rune(p.Name)) > 40 {
			return fmt.Errorf("the preset name %q is too long (40 characters at most)", p.Name)
		}
		key := strings.ToLower(p.Name)
		if names[key] {
			return fmt.Errorf("there are two presets called %q", p.Name)
		}
		names[key] = true

		sites := make([]string, 0, len(p.Sites))
		for _, raw := range p.Sites {
			site := CleanSite(raw)
			if site == "" {
				continue
			}
			if !siteRe.MatchString(site) {
				return fmt.Errorf("%q in the %s preset isn't a site name like music.youtube.com", strings.TrimSpace(raw), p.Name)
			}
			if prev, ok := owner[site]; ok {
				if prev == p.Name {
					continue
				}
				return fmt.Errorf("%s is set for both the %s and %s presets", site, prev, p.Name)
			}
			owner[site] = p.Name
			sites = append(sites, site)
		}
		p.Sites = sites

		o := &p.Options
		o.SectionStart, o.SectionEnd, o.Chapters = "", "", nil
		o.IgnoreArchive, o.LiveFromStart, o.WaitForVideo, o.AudioLang = false, false, false, ""
		if o.Preset == "custom" {
			// A format picked from one video's list means nothing for the next.
			o.Preset, o.FormatID = "best", ""
		}
		if err := ValidateOptions(o); err != nil {
			return fmt.Errorf("the %s preset: %w", p.Name, err)
		}
	}
	return nil
}

// CleanSite turns what was typed as a preset's site into a host name: it drops a scheme, a path,
// a port and a leading "www.", and lowercases it. "https://www.Example.com/x" -> "example.com".
func CleanSite(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "www."), ".")
	return s
}

// Store persists Settings under the user's config directory and is safe for concurrent use.
type Store struct {
	mu   sync.RWMutex
	dir  string
	data Settings
	// readErr is set when settings.json exists but couldn't be read. Saving is refused so the
	// defaults we fell back to never overwrite the user's real file.
	readErr error
}

// Open loads settings from dir (creating it if needed). A missing or corrupt file yields
// defaults; a file with some invalid values keeps the valid ones.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	st := &Store{dir: dir, data: Default()}
	raw, err := ReadFileRetry(st.path())
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		st.readErr = err
	default:
		loaded := Default()
		// Saved presets are decoded on their own: decoded over the starter presets, json would
		// fill each one's missing fields from the starter in the same position.
		loaded.Presets = nil
		if json.Unmarshal(raw, &loaded) != nil {
			// Keep the unreadable file so the user can recover values from it.
			_ = os.Rename(st.path(), st.path()+".corrupt")
			break
		}
		if loaded.Presets == nil {
			loaded.Presets = StarterPresets() // a file from before presets
		}
		if loaded.Validate() != nil {
			// A hand-edited file with bad values must not stop the app from starting. Keep a copy,
			// since the next save rewrites it.
			_ = WriteFileAtomic(st.path()+".invalid", raw, 0o600)
			loaded = repair(loaded)
		}
		st.data = loaded
	}
	return st, nil
}

// ReadFileRetry reads path, retrying briefly: antivirus and sync tools hold files open for a
// moment. A missing file is reported at once.
func ReadFileRetry(path string) ([]byte, error) {
	var err error
	for i := 0; i < 5; i++ {
		var raw []byte
		if raw, err = os.ReadFile(path); err == nil || errors.Is(err, fs.ErrNotExist) {
			return raw, err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, err
}

// repair keeps every field of s that is valid on its own and resets the rest to defaults, so
// one bad value doesn't cost the user all their other settings.
func repair(s Settings) Settings {
	out := Default()
	keep := func(set func(*Settings)) {
		try := clone(out)
		set(&try)
		if try.Validate() == nil {
			out = try
		}
	}
	src := reflect.ValueOf(s)
	defs := src.FieldByName("Defaults")
	// Two passes, so a field that is only valid next to another (preset "custom" needs a
	// format) is picked up once its partner is in place.
	for pass := 0; pass < 2; pass++ {
		for i := 0; i < src.NumField(); i++ {
			if name := src.Type().Field(i).Name; name != "Defaults" && name != "Presets" {
				keep(func(t *Settings) { reflect.ValueOf(t).Elem().Field(i).Set(src.Field(i)) })
			}
		}
		for i := 0; i < defs.NumField(); i++ {
			keep(func(t *Settings) { reflect.ValueOf(&t.Defaults).Elem().Field(i).Set(defs.Field(i)) })
		}
	}
	// Presets one by one, so a bad one costs only itself.
	out.Presets = []NamedPreset{}
	for _, p := range s.Presets {
		keep(func(t *Settings) { t.Presets = append(t.Presets, p) })
	}
	return clone(out)
}

// DefaultDir is where the app keeps its files.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ConfigDirName), nil
}

func (s *Store) path() string { return filepath.Join(s.dir, "settings.json") }

// Dir is the app's data directory.
func (s *Store) Dir() string { return s.dir }

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.data)
}

// Set validates and saves new settings.
func (s *Store) Set(next Settings) (Settings, error) {
	if err := next.Validate(); err != nil {
		return Settings{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(next); err != nil {
		return Settings{}, err
	}
	s.data = next
	return clone(next), nil
}

// Update applies fn to a copy of the settings and saves the result.
func (s *Store) Update(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data)
	fn(&next)
	if err := next.Validate(); err != nil {
		return Settings{}, err
	}
	if err := s.write(next); err != nil {
		return Settings{}, err
	}
	s.data = next
	return clone(next), nil
}

func (s *Store) write(v Settings) error {
	if s.readErr != nil {
		return fmt.Errorf("settings.json couldn't be read when the app started (%v), so changes can't be saved until it restarts", s.readErr)
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.path(), raw, 0o600)
}

func clone(v Settings) Settings {
	v.Defaults = cloneOptions(v.Defaults)
	presets := make([]NamedPreset, len(v.Presets))
	for i, p := range v.Presets {
		p.Sites = append([]string{}, p.Sites...)
		p.Options = cloneOptions(p.Options)
		presets[i] = p
	}
	v.Presets = presets
	return v
}

func cloneOptions(o DownloadOptions) DownloadOptions {
	o.Subtitles.Langs = append([]string{}, o.Subtitles.Langs...)
	o.SponsorBlock = append([]string{}, o.SponsorBlock...)
	o.Chapters = append([]string{}, o.Chapters...)
	return o
}

// WriteFileAtomic writes via a temp file and rename so a crash never leaves a half-written file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	// Without a flush, a power cut after the rename can leave an empty file behind.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, perm)
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// PresetFor returns the preset whose sites cover link's host, or nil. A site also covers its
// subdomains, and the longest match wins: music.youtube.com beats youtube.com. The Download page
// does the same (frontend/src/presets.ts).
func PresetFor(link string, presets []NamedPreset) *NamedPreset {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return nil
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	var best *NamedPreset
	bestLen := 0
	for i := range presets {
		for _, site := range presets[i].Sites {
			if (host == site || strings.HasSuffix(host, "."+site)) && len(site) > bestLen {
				best, bestLen = &presets[i], len(site)
			}
		}
	}
	return best
}

// FindPreset returns the preset called name (ignoring case), or nil.
func FindPreset(name string, presets []NamedPreset) *NamedPreset {
	for i := range presets {
		if strings.EqualFold(presets[i].Name, strings.TrimSpace(name)) {
			return &presets[i]
		}
	}
	return nil
}

// ActiveHours limits when downloads start: overnight on a data cap, say. Downloads running when
// the window closes finish; the rest wait for it to open again. From after To crosses midnight.
type ActiveHours struct {
	On   bool   `json:"on"`
	From string `json:"from"` // "01:00"
	To   string `json:"to"`   // "07:00"
}

var clockRe = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)

func clockMinutes(s string) (int, bool) {
	m := clockRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	h, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	return h*60 + mm, true
}

func (h *ActiveHours) validate() error {
	h.From, h.To = strings.TrimSpace(h.From), strings.TrimSpace(h.To)
	if h.From == "" && h.To == "" {
		h.From, h.To = "01:00", "07:00"
	}
	from, ok1 := clockMinutes(h.From)
	to, ok2 := clockMinutes(h.To)
	if !ok1 || !ok2 {
		return errors.New("download hours are times like 01:00 and 07:00")
	}
	if h.On && from == to {
		return errors.New("download hours start and end at the same time")
	}
	return nil
}

// Open reports whether downloads may start at now, and if not, when they may next.
func (h ActiveHours) Open(now time.Time) (bool, time.Time) {
	from, ok1 := clockMinutes(h.From)
	to, ok2 := clockMinutes(h.To)
	if !h.On || !ok1 || !ok2 || from == to {
		return true, time.Time{}
	}
	cur := now.Hour()*60 + now.Minute()
	in := from < to && cur >= from && cur < to || from > to && (cur >= from || cur < to)
	if in {
		return true, time.Time{}
	}
	// By the clock, not by adding minutes to midnight: on a day the clocks change, that's an
	// hour off.
	next := time.Date(now.Year(), now.Month(), now.Day(), from/60, from%60, 0, 0, now.Location())
	if !next.After(now) {
		next = time.Date(now.Year(), now.Month(), now.Day()+1, from/60, from%60, 0, 0, now.Location())
	}
	return false, next
}
