package ytdlp

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"yt-dlp-gui/internal/settings"
)

// Env is everything needed to start yt-dlp: the binary plus the settings that affect every run.
type Env struct {
	Bin         string
	Settings    settings.Settings
	FFmpeg      string // path to ffmpeg, "" if none was found
	JSRuntimes  []JSRuntime
	CookieFile  string // a private copy of the cookies file for this run ("" = none)
	ArchiveFile string // download archive path, used when Settings.UseArchive is on
}

// Markers that prefix the lines the GUI asks yt-dlp to print, so they can't be confused
// with ordinary log output.
const (
	MarkProgress = "YTGUI_P "
	MarkPost     = "YTGUI_PP "
	MarkPlan     = "YTGUI_PLAN "
	MarkStreams  = "YTGUI_STREAMS "
	MarkFile     = "YTGUI_FILE\t"
)

// CommonArgs are applied to every invocation (probing and downloading).
func (e Env) CommonArgs() []string {
	s := e.Settings
	a := []string{"--ignore-config", "--encoding", "utf-8", "--color", "never", "--socket-timeout", "30"}
	if s.Proxy != "" {
		a = append(a, "--proxy="+s.Proxy)
	}
	switch s.Cookies.Mode {
	case "browser":
		spec := s.Cookies.Browser
		if s.Cookies.Profile != "" {
			spec += ":" + s.Cookies.Profile
		}
		a = append(a, "--cookies-from-browser="+spec)
	case "file":
		if e.CookieFile != "" {
			a = append(a, "--cookies="+e.CookieFile)
		}
	}
	for _, rt := range e.JSRuntimes {
		a = append(a, "--js-runtimes="+rt.Name+":"+rt.Path)
	}
	if e.FFmpeg != "" {
		a = append(a, "--ffmpeg-location="+e.FFmpeg)
	}
	return a
}

// UserArgs are the user's own "extra arguments". They go last so they can override anything
// the GUI set. A malformed string is rejected when settings are saved, so errors are ignored here.
func (e Env) UserArgs() []string {
	extra, _ := SplitArgs(e.Settings.ExtraArgs)
	return extra
}

// Target identifies what to download and where it sits inside a playlist.
type Target struct {
	URL           string
	PlaylistTitle string
	PlaylistIndex int
	PlaylistCount int
}

// DownloadArgs builds the full yt-dlp command line (without the executable) for one download.
func (e Env) DownloadArgs(o settings.DownloadOptions, t Target) []string {
	s := e.Settings
	a := []string{"--newline", "--progress", "--no-playlist", "--no-quiet"}
	a = append(a, e.CommonArgs()...)

	dir := o.OutputDir
	if dir == "" {
		dir = s.DownloadDir
	}
	a = append(a, "--paths="+dir, "--output="+OutputTemplate(s.FilenameTemplate, o, t))
	a = append(a, "--trim-filenames=180")

	a = append(a, FormatArgs(o)...)
	a = append(a, SubtitleArgs(o.Subtitles)...)

	if o.EmbedMetadata {
		a = append(a, "--embed-metadata")
	}
	if o.EmbedChapters {
		a = append(a, "--embed-chapters")
	}
	// WebM can't hold a thumbnail and the post-processor would fail the whole download. (With
	// no container chosen, yt-dlp itself switches to MKV for this.)
	if o.EmbedThumbnail && !(o.Container == "webm" && o.Preset != "audio") {
		a = append(a, "--embed-thumbnail")
	}
	if o.WriteThumbnail {
		a = append(a, "--write-thumbnail")
	}
	if o.EmbedThumbnail || o.WriteThumbnail {
		// WebP thumbnails can't be embedded in most containers.
		a = append(a, "--convert-thumbnails=jpg")
	}
	if o.WriteDescription {
		a = append(a, "--write-description")
	}
	if o.WriteInfoJSON {
		a = append(a, "--write-info-json")
	}
	if len(o.SponsorBlock) > 0 {
		a = append(a, "--sponsorblock-remove="+strings.Join(o.SponsorBlock, ","))
	}
	if o.SectionStart != "" || o.SectionEnd != "" {
		start, end := o.SectionStart, o.SectionEnd
		if start == "" {
			start = "0"
		}
		if end == "" {
			end = "inf"
		}
		a = append(a, "--download-sections=*"+start+"-"+end)
	}

	if s.RateLimit != "" {
		a = append(a, "--limit-rate="+s.RateLimit)
	}
	a = append(a,
		"--concurrent-fragments="+strconv.Itoa(s.ConcurrentFragments),
		"--retries="+strconv.Itoa(s.Retries),
		"--fragment-retries="+strconv.Itoa(s.Retries),
	)
	if s.UseArchive && e.ArchiveFile != "" {
		a = append(a, "--download-archive="+e.ArchiveFile)
	}

	a = append(a,
		"--progress-template=download:"+MarkProgress+"%(progress)j",
		"--progress-template=postprocess:"+MarkPost+"%(progress.postprocessor)s|%(progress.status)s",
		"--print=before_dl:"+MarkPlan+"%(.{id,title,uploader,thumbnail,duration,filesize,filesize_approx})j",
		"--print=before_dl:"+MarkStreams+"%(requested_formats.:.{filesize,filesize_approx,vcodec,acodec})j",
		"--print=after_move:"+MarkFile+"%(filepath)s",
	)
	a = append(a, e.UserArgs()...)
	a = append(a, "--", t.URL)
	return a
}

// QualitySort ranks video streams by resolution, frame rate, HDR and then bitrate, ahead of
// yt-dlp's default codec preference. Observed: signed in, YouTube offers two "1080p Premium"
// streams, VP9 at ~2.8 Mbps and AV1 at ~0.4 Mbps, and the default ranking picked the AV1 one.
const QualitySort = "res,fps,hdr:12,vbr"

// FormatArgs maps the quality preset onto yt-dlp's format selection flags.
func FormatArgs(o settings.DownloadOptions) []string {
	var a []string
	switch o.Preset {
	case "audio":
		a = append(a, "--format=ba/b", "--extract-audio", "--audio-format="+o.AudioFormat)
		switch o.AudioFormat {
		case "mp3", "aac", "opus", "m4a":
			if o.AudioQuality != "" {
				a = append(a, "--audio-quality="+o.AudioQuality)
			}
		}
		return a
	case "custom":
		a = append(a, "--format="+o.FormatID)
	case "best", "":
		a = append(a, "--format=bv*+ba/b", "--format-sort="+QualitySort)
	default:
		h := o.Preset
		// "<=?" also admits formats whose height is unknown (some sites don't report it).
		a = append(a, fmt.Sprintf("--format=bv*[height<=%[1]s]+ba/b[height<=%[1]s]/bv*[height<=?%[1]s]+ba/b[height<=?%[1]s]", h), "--format-sort="+QualitySort)
	}
	switch o.Container {
	case "":
	case "webm":
		// WebM only holds VP8/VP9/AV1 with Opus/Vorbis. Anything else (h264 is common outside
		// YouTube) goes into MKV rather than failing in ffmpeg.
		a = append(a, "--merge-output-format=webm/mkv", "--remux-video=webm>webm/mkv")
	default:
		a = append(a, "--merge-output-format="+o.Container, "--remux-video="+o.Container)
	}
	return a
}

// SubtitleArgs maps subtitle options onto flags. Embed-only mode omits --write-subs: with it
// yt-dlp keeps the sidecar files after embedding them.
func SubtitleArgs(s settings.SubtitleOptions) []string {
	if s.Mode == "" || s.Mode == "none" {
		return nil
	}
	a := []string{"--sub-langs=" + strings.Join(s.Langs, ",")}
	switch {
	case s.Mode == "files" || s.Mode == "both":
		a = append(a, "--write-subs")
	case s.Auto:
		// --embed-subs only implies --write-subs when auto captions are off; without it yt-dlp
		// embeds the auto captions even where uploaded subtitles exist. no-keep-subs then
		// deletes the sidecar files that --write-subs would otherwise keep.
		a = append(a, "--write-subs", "--compat-options=no-keep-subs")
	}
	if s.Mode == "embed" || s.Mode == "both" {
		a = append(a, "--embed-subs")
	}
	if s.Auto {
		a = append(a, "--write-auto-subs")
	}
	if s.Format != "" {
		a = append(a, "--convert-subs="+s.Format)
	}
	return a
}

// OutputTemplate builds the -o template, adding playlist folder and numbering prefixes.
// Playlist values are known to the GUI (not to yt-dlp, which downloads one video at a time),
// so they are baked in as literal text with % escaped.
func OutputTemplate(base string, o settings.DownloadOptions, t Target) string {
	if base == "" {
		base = settings.DefaultTemplate
	}
	prefix := ""
	if t.PlaylistTitle != "" && o.PlaylistFolder {
		prefix += sanitizeSegment(t.PlaylistTitle) + "/"
	}
	if t.PlaylistIndex > 0 && o.PlaylistNumbering {
		width := len(strconv.Itoa(t.PlaylistCount))
		if width < 2 {
			width = 2
		}
		prefix += fmt.Sprintf("%0*d - ", width, t.PlaylistIndex)
	}
	return prefix + base
}

var reservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// sanitizeSegment makes text safe as a single folder name on every OS and safe inside a
// yt-dlp output template.
func sanitizeSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '%':
			b.WriteString("%%")
		case strings.ContainsRune(`<>:"/\|?*`, r), unicode.IsControl(r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if runes := []rune(out); len(runes) > 100 {
		out = strings.TrimRight(string(runes[:100]), ". ")
	}
	if out == "" {
		return "playlist"
	}
	if reservedNames[strings.ToLower(out)] {
		out += "_"
	}
	return out
}

// childEnv forces UTF-8 output from Python so non-Latin titles survive the pipe.
func childEnv() []string {
	return append(os.Environ(), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8")
}

// SplitArgs splits an "extra arguments" string the way a shell would, honouring single and
// double quotes but not backslashes (they appear in Windows paths).
func SplitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inArg = true
		case unicode.IsSpace(r):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in extra arguments", quote)
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}
