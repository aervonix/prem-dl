// Package settings defines the user's preferences and per-download options, and persists them.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ConfigDirName is the folder created under the user's config directory.
const ConfigDirName = "yt-dlp-gui"

const DefaultTemplate = "%(title)s [%(id)s].%(ext)s"

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
	Preset       string `json:"preset"`
	FormatID     string `json:"formatId"`     // yt-dlp -f expression, used when Preset == custom
	Container    string `json:"container"`    // "" | mp4 | mkv | webm
	AudioFormat  string `json:"audioFormat"`  // best | mp3 | m4a | opus | flac | wav | aac
	AudioQuality string `json:"audioQuality"` // "0" (best) .. "10" (worst)

	Subtitles SubtitleOptions `json:"subtitles"`

	EmbedMetadata    bool `json:"embedMetadata"`
	EmbedThumbnail   bool `json:"embedThumbnail"`
	EmbedChapters    bool `json:"embedChapters"`
	WriteDescription bool `json:"writeDescription"`
	WriteInfoJSON    bool `json:"writeInfoJson"`
	WriteThumbnail   bool `json:"writeThumbnail"`

	SponsorBlock []string `json:"sponsorBlock"` // categories to cut out

	SectionStart string `json:"sectionStart"` // e.g. 1:30 — empty means from the start
	SectionEnd   string `json:"sectionEnd"`   // empty means until the end

	OutputDir         string `json:"outputDir"` // "" uses Settings.DownloadDir
	PlaylistFolder    bool   `json:"playlistFolder"`
	PlaylistNumbering bool   `json:"playlistNumbering"`
	// IgnoreArchive downloads the video even when the download archive lists it.
	IgnoreArchive bool `json:"ignoreArchive"`
	// SponsorBlockMark marks the SponsorBlock segments as chapters instead of cutting them out.
	SponsorBlockMark bool `json:"sponsorBlockMark"`
	// SplitChapters also saves each chapter as its own file, in a folder named after the video.
	SplitChapters bool `json:"splitChapters"`
	// LiveFromStart records a live stream from its beginning rather than from now.
	LiveFromStart bool `json:"liveFromStart"`
	// AudioLang picks the audio track by language ("en", "es-419") on dubbed videos; "" takes
	// the default. Formats without it fall back to the default track.
	AudioLang string `json:"audioLang"`
}

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
	Retries             int             `json:"retries"`
	AutoRetry           bool            `json:"autoRetry"`     // retry downloads that failed with a passing problem
	SleepRequests       float64         `json:"sleepRequests"` // seconds between requests while a download reads the site
	SleepInterval       float64         `json:"sleepInterval"` // seconds to wait before each download starts
	UseArchive          bool            `json:"useArchive"`    // skip videos already downloaded before
	Cookies             Cookies         `json:"cookies"`
	AutoUpdate          bool            `json:"autoUpdate"`
	UpdateChannel       string          `json:"updateChannel"` // stable | nightly | master
	LastUpdateCheck     time.Time       `json:"lastUpdateCheck" ts_type:"string"`
	LatestYtdlp         string          `json:"latestYtdlp"`        // newest release the last successful check found
	LatestYtdlpChannel  string          `json:"latestYtdlpChannel"` // the channel that check asked about
	YtdlpPath           string          `json:"ytdlpPath"`
	FfmpegPath          string          `json:"ffmpegPath"`
	Theme               string          `json:"theme"`       // system | light | dark
	NotifySound         bool            `json:"notifySound"` // a click when a download finishes
	NotifyPopup         bool            `json:"notifyPopup"` // a card in the corner when one finishes
	ExtraArgs           string          `json:"extraArgs"`
	Defaults            DownloadOptions `json:"defaults"`
}

// Default returns first-run settings.
func Default() Settings {
	dir, _ := os.UserHomeDir()
	if dir != "" {
		dir = filepath.Join(dir, "Downloads")
	}
	return Settings{
		DownloadDir:         dir,
		FilenameTemplate:    DefaultTemplate,
		MaxConcurrent:       2,
		ConcurrentFragments: 4,
		Retries:             10,
		AutoRetry:           true,
		UseArchive:          false,
		Cookies:             Cookies{Mode: "none", Browser: "firefox"},
		AutoUpdate:          true,
		UpdateChannel:       "stable",
		Theme:               "system",
		NotifySound:         true,
		NotifyPopup:         true,
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
	}
}

var (
	rateLimitRe = regexp.MustCompile(`^\d+(\.\d+)?[KkMmGg]?$`)
	timeRe      = regexp.MustCompile(`^(\d+:){0,2}\d+(\.\d+)?$`)
	audioLangRe = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{1,8})*$`)
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

// ValidateOptions normalises the options in place and rejects values that would make
// yt-dlp misbehave. The returned error message is shown to the user.
func ValidateOptions(o *DownloadOptions) error {
	if !oneOf(o.Preset, "best", "2160", "1440", "1080", "720", "480", "360", "audio", "custom") {
		o.Preset = "best"
	}
	o.FormatID = strings.TrimSpace(o.FormatID)
	if o.Preset == "custom" && o.FormatID == "" {
		return errors.New("choose a format, or switch to a quality preset")
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
	if !strings.Contains(s.FilenameTemplate, "%(ext)s") {
		return errors.New("the filename template must end with %(ext)s so files keep their extension")
	}
	if strings.HasPrefix(s.FilenameTemplate, "-") {
		return errors.New("the filename template can't start with a dash")
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
	if s.Cookies.Mode == "file" && strings.TrimSpace(s.Cookies.File) == "" {
		return errors.New("choose a cookies.txt file, or switch the cookie source")
	}
	if !oneOf(s.UpdateChannel, "stable", "nightly", "master") {
		s.UpdateChannel = "stable"
	}
	if !oneOf(s.Theme, "system", "light", "dark") {
		s.Theme = "system"
	}
	return ValidateOptions(&s.Defaults)
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
		if json.Unmarshal(raw, &loaded) != nil {
			// Keep the unreadable file so the user can recover values from it.
			_ = os.Rename(st.path(), st.path()+".corrupt")
			break
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
			if src.Type().Field(i).Name != "Defaults" {
				keep(func(t *Settings) { reflect.ValueOf(t).Elem().Field(i).Set(src.Field(i)) })
			}
		}
		for i := 0; i < defs.NumField(); i++ {
			keep(func(t *Settings) { reflect.ValueOf(&t.Defaults).Elem().Field(i).Set(defs.Field(i)) })
		}
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
	v.Defaults.Subtitles.Langs = append([]string{}, v.Defaults.Subtitles.Langs...)
	v.Defaults.SponsorBlock = append([]string{}, v.Defaults.SponsorBlock...)
	return v
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
