package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/sys"
)

// FormatInfo is one downloadable stream of a video.
type FormatInfo struct {
	ID         string  `json:"id"`
	Ext        string  `json:"ext"`
	Resolution string  `json:"resolution"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        float64 `json:"fps"`
	VCodec     string  `json:"vcodec"`
	ACodec     string  `json:"acodec"`
	Note       string  `json:"note"`
	Filesize   int64   `json:"filesize"` // exact or approximate; 0 when unknown
	TBR        float64 `json:"tbr"`
	HasVideo   bool    `json:"hasVideo"`
	HasAudio   bool    `json:"hasAudio"`
}

// SubtitleTrack is an available subtitle language.
type SubtitleTrack struct {
	Lang string `json:"lang"`
	Name string `json:"name"`
	Auto bool   `json:"auto"`
}

// VideoInfo describes a single video.
type VideoInfo struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	URL          string          `json:"url"`
	Uploader     string          `json:"uploader"`
	Thumbnail    string          `json:"thumbnail"`
	Duration     float64         `json:"duration"`
	UploadDate   string          `json:"uploadDate"`
	ViewCount    int64           `json:"viewCount"`
	IsLive       bool            `json:"isLive"`
	Extractor    string          `json:"extractor"`
	Formats      []FormatInfo    `json:"formats"`
	Heights      []int           `json:"heights"`
	Subtitles    []SubtitleTrack `json:"subtitles"`
	AutoCaptions []SubtitleTrack `json:"autoCaptions"`
	// BestPremium is set when the default format choice, which the "Best" preset uses, picks a
	// YouTube Premium video stream (format_note "Premium").
	BestPremium bool `json:"bestPremium"`
}

// PlaylistEntry is one item of a playlist.
type PlaylistEntry struct {
	Index       int     `json:"index"` // 1-based position in the (flattened) playlist
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Uploader    string  `json:"uploader"`
	Thumbnail   string  `json:"thumbnail"`
	Duration    float64 `json:"duration"`
	Group       string  `json:"group"` // nested playlist (e.g. a channel tab) this came from
	Unavailable bool    `json:"unavailable"`
	// IsPlaylist marks an entry that is itself a playlist (a channel's Playlists tab lists them
	// as plain links). It has to be analysed on its own; queued as a video it would download the
	// whole playlist as one job.
	IsPlaylist bool `json:"isPlaylist"`
}

// PlaylistInfo describes a playlist, channel or similar collection.
type PlaylistInfo struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	URL      string          `json:"url"`
	Uploader string          `json:"uploader"`
	Entries  []PlaylistEntry `json:"entries"`
}

// ProbeResult is what the UI shows after analysing a link.
type ProbeResult struct {
	Kind     string        `json:"kind"` // video | playlist
	Video    *VideoInfo    `json:"video"`
	Playlist *PlaylistInfo `json:"playlist"`
}

// NormalizeURL trims the input, adds a scheme when it was left off, and rejects anything yt-dlp
// could mistake for an option.
func NormalizeURL(raw string) (string, error) {
	u := strings.TrimSpace(raw)
	if u == "" {
		return "", errors.New("enter a link first")
	}
	if strings.ContainsAny(u, " \t\r\n") {
		return "", fmt.Errorf("%q contains spaces. Put one link per line.", u)
	}
	if !strings.Contains(u, "://") {
		if strings.HasPrefix(u, "-") || !strings.Contains(u, ".") {
			return "", fmt.Errorf("%q doesn't look like a link", u)
		}
		u = "https://" + u
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%q isn't a valid http(s) link", raw)
	}
	return u, nil
}

type rawThumb struct {
	URL   string  `json:"url"`
	Width float64 `json:"width"`
}

type rawSub struct {
	Name string `json:"name"`
}

// Numbers are decoded as float64 even where only integers make sense: some extractors emit
// 720.0 or a fractional filesize_approx, and one such value must not fail the whole probe.
type rawFormat struct {
	FormatID       string  `json:"format_id"`
	Ext            string  `json:"ext"`
	Resolution     string  `json:"resolution"`
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
	FPS            float64 `json:"fps"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	FormatNote     string  `json:"format_note"`
	Filesize       float64 `json:"filesize"`
	FilesizeApprox float64 `json:"filesize_approx"`
	TBR            float64 `json:"tbr"`
}

type rawInfo struct {
	Type       string     `json:"_type"`
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	URL        string     `json:"url"`
	WebpageURL string     `json:"webpage_url"`
	Uploader   string     `json:"uploader"`
	Channel    string     `json:"channel"`
	Thumbnail  string     `json:"thumbnail"`
	Thumbnails []rawThumb `json:"thumbnails"`
	Duration   float64    `json:"duration"`
	UploadDate string     `json:"upload_date"`
	ViewCount  float64    `json:"view_count"`
	IsLive     bool       `json:"is_live"`
	Extractor  string     `json:"extractor_key"`
	IEKey      string     `json:"ie_key"` // flat entries: the extractor that will handle URL

	Availability string `json:"availability"`

	Formats           []rawFormat         `json:"formats"`
	FormatNote        string              `json:"format_note"`       // of the default choice
	RequestedFormats  []rawFormat         `json:"requested_formats"` // the default choice, when merged
	Subtitles         map[string][]rawSub `json:"subtitles"`
	AutomaticCaptions map[string][]rawSub `json:"automatic_captions"`
	Entries           []json.RawMessage   `json:"entries"`
}

func (r *rawInfo) uploader() string {
	if r.Uploader != "" {
		return r.Uploader
	}
	return r.Channel
}

// pickThumb prefers the smallest thumbnail that is still at least 240px wide.
func pickThumb(r *rawInfo) string {
	if r.Thumbnail != "" {
		return r.Thumbnail
	}
	best, bestW := "", 0.0
	for _, t := range r.Thumbnails {
		if t.URL == "" {
			continue
		}
		switch {
		case best == "":
			best, bestW = t.URL, t.Width
		case bestW < 240 && t.Width > bestW:
			best, bestW = t.URL, t.Width
		case t.Width >= 240 && t.Width < bestW:
			best, bestW = t.URL, t.Width
		}
	}
	return best
}

// decodeInfo tolerates fields of an unexpected type, which vary by extractor (a numeric id, a
// width of 1280.0): encoding/json still fills in everything else and only reports the mismatch,
// which must not cost the whole answer.
func decodeInfo(data []byte, r *rawInfo) error {
	err := json.Unmarshal(data, r)
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return nil
	}
	return err
}

// ParseProbe turns the JSON printed by `yt-dlp -J --flat-playlist` into a ProbeResult.
func ParseProbe(data []byte) (*ProbeResult, error) {
	var raw rawInfo
	if err := decodeInfo(data, &raw); err != nil {
		return nil, fmt.Errorf("couldn't read yt-dlp's answer: %w", err)
	}
	if raw.Type == "playlist" || raw.Type == "multi_video" || (raw.Type == "" && len(raw.Entries) > 0) {
		pl := &PlaylistInfo{ID: raw.ID, Title: raw.Title, URL: raw.WebpageURL, Uploader: raw.uploader()}
		if pl.URL == "" {
			pl.URL = raw.URL
		}
		collectEntries(raw.Entries, "", 0, &pl.Entries)
		for i := range pl.Entries {
			pl.Entries[i].Index = i + 1
		}
		return &ProbeResult{Kind: "playlist", Playlist: pl}, nil
	}
	return &ProbeResult{Kind: "video", Video: buildVideo(&raw)}, nil
}

func collectEntries(entries []json.RawMessage, group string, depth int, out *[]PlaylistEntry) {
	for _, e := range entries {
		var r rawInfo
		// A null entry is how yt-dlp marks an item it could not list. Unmarshalling null into a
		// struct succeeds and leaves it zeroed, so it has to be checked for explicitly.
		if bytes.Equal(bytes.TrimSpace(e), []byte("null")) || decodeInfo(e, &r) != nil {
			continue
		}
		if len(r.Entries) > 0 && depth < 3 {
			g := r.Title
			if g == "" {
				g = group
			}
			collectEntries(r.Entries, g, depth+1, out)
			continue
		}
		u := r.WebpageURL
		if u == "" {
			u = r.URL
		}
		entry := PlaylistEntry{
			ID: r.ID, Title: r.Title, URL: u, Uploader: r.uploader(),
			Thumbnail: pickThumb(&r), Duration: r.Duration, Group: group,
			IsPlaylist: isCollection(&r, u),
		}
		if entry.Title == "" {
			entry.Title = entry.ID
		}
		lower := strings.ToLower(r.Title)
		if !strings.HasPrefix(u, "http") ||
			strings.HasPrefix(lower, "[private") || strings.HasPrefix(lower, "[deleted") ||
			r.Availability == "private" {
			entry.Unavailable = true
		}
		*out = append(*out, entry)
	}
}

// isCollection reports whether a flat entry points at another playlist rather than a video.
// Observed on a channel's Playlists tab: {"_type": "url", "ie_key": "YoutubeTab",
// "url": "https://www.youtube.com/playlist?list=..."}.
func isCollection(r *rawInfo, link string) bool {
	switch r.Type {
	case "playlist", "multi_video":
		return true // nested deeper than collectEntries expands
	case "url", "url_transparent":
	default:
		return false
	}
	for _, s := range []string{"Tab", "Playlist", "Album", "Channel"} {
		if strings.Contains(r.IEKey, s) {
			return true
		}
	}
	if u, err := url.Parse(link); err == nil {
		q := u.Query()
		return q.Has("list") && !q.Has("v")
	}
	return false
}

func buildVideo(r *rawInfo) *VideoInfo {
	v := &VideoInfo{
		ID: r.ID, Title: r.Title, URL: r.WebpageURL, Uploader: r.uploader(), Thumbnail: pickThumb(r),
		Duration: r.Duration, UploadDate: r.UploadDate, ViewCount: int64(r.ViewCount), IsLive: r.IsLive,
		Extractor: r.Extractor,
		Formats:   []FormatInfo{}, Heights: []int{}, Subtitles: []SubtitleTrack{}, AutoCaptions: []SubtitleTrack{},
	}
	if v.URL == "" {
		v.URL = r.URL
	}
	seenHeight := map[int]bool{}
	for _, f := range r.Formats {
		hasV := f.VCodec != "" && f.VCodec != "none"
		hasA := f.ACodec != "" && f.ACodec != "none"
		// Storyboards (mhtml) and other pseudo-formats have neither stream.
		if f.Ext == "mhtml" || (!hasV && !hasA && f.VCodec != "") {
			continue
		}
		size := int64(f.Filesize)
		if size == 0 {
			size = int64(f.FilesizeApprox)
		}
		height := int(f.Height)
		v.Formats = append(v.Formats, FormatInfo{
			ID: f.FormatID, Ext: f.Ext, Resolution: f.Resolution, Width: int(f.Width), Height: height,
			FPS: f.FPS, VCodec: f.VCodec, ACodec: f.ACodec, Note: f.FormatNote, Filesize: size,
			TBR: f.TBR, HasVideo: hasV, HasAudio: hasA,
		})
		if hasV && height > 0 && !seenHeight[height] {
			seenHeight[height] = true
			v.Heights = append(v.Heights, height)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(v.Heights)))
	v.BestPremium = isPremium(r.FormatNote)
	for _, f := range r.RequestedFormats {
		if f.VCodec != "" && f.VCodec != "none" {
			v.BestPremium = isPremium(f.FormatNote)
		}
	}
	v.Subtitles = tracks(r.Subtitles, false)
	v.AutoCaptions = tracks(r.AutomaticCaptions, true)
	return v
}

func isPremium(note string) bool { return strings.Contains(strings.ToLower(note), "premium") }

func tracks(m map[string][]rawSub, auto bool) []SubtitleTrack {
	out := make([]SubtitleTrack, 0, len(m))
	for lang, subs := range m {
		if lang == "live_chat" { // chat replay is not a subtitle
			continue
		}
		name := lang
		if len(subs) > 0 && subs[0].Name != "" {
			name = subs[0].Name
		}
		out = append(out, SubtitleTrack{Lang: lang, Name: name, Auto: auto})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lang < out[j].Lang })
	return out
}

// Probe analyses a link without downloading it. Failures are *apperr.AppError.
func Probe(ctx context.Context, env Env, rawURL string, noPlaylist bool) (*ProbeResult, error) {
	link, err := NormalizeURL(rawURL)
	if err != nil {
		return nil, apperr.New("invalid_url", "That doesn't look like a link", err.Error(),
			"Paste a full link starting with http:// or https://.", apperr.ActionNone)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// The default format choice in the JSON is what "Best" downloads, so rank it the same way.
	args := append([]string{"--dump-single-json", "--flat-playlist", "--no-warnings", "--format-sort=" + QualitySort}, env.CommonArgs()...)
	if noPlaylist {
		args = append(args, "--no-playlist")
	}
	args = append(args, env.UserArgs()...)
	args = append(args, "--", link)

	cmd := exec.CommandContext(ctx, env.Bin, args...)
	sys.Quiet(cmd)
	cmd.Env = childEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Start()
	if runErr == nil {
		sys.Contain(cmd)
		runErr = cmd.Wait()
	}

	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, apperr.New("timeout", "Analysis timed out",
				"yt-dlp took longer than 5 minutes to read this link.", "Check your connection and retry.", apperr.ActionNone)
		}
		return nil, context.Canceled
	}
	if runErr != nil || stdout.Len() == 0 {
		return nil, apperr.Classify(splitLines(stderr.String()), runErr)
	}
	res, perr := ParseProbe(stdout.Bytes())
	if perr != nil {
		return nil, apperr.Classify(splitLines(stderr.String()), perr)
	}
	return res, nil
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
