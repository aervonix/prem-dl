package ytdlp

import (
	"fmt"
	"os"
	"regexp"
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
	// PluginDir is a yt-dlp plugin folder to load besides the default ones, and Vars are
	// environment variables for the run (Prem-dl's challenge solver, internal/jsc); both empty
	// when it isn't used.
	PluginDir string
	Vars      []string
}

// Markers that prefix the lines the GUI asks yt-dlp to print, so they can't be confused
// with ordinary log output.
const (
	MarkProgress = "YTGUI_P "
	MarkPost     = "YTGUI_PP "
	MarkPlan     = "YTGUI_PLAN "
	MarkStreams  = "YTGUI_STREAMS "
	MarkFile     = "YTGUI_FILE\t"
	MarkName     = "YTGUI_NAME " // the probe's expansion of the filename template
)

// CommonArgs are applied to every invocation (probing and downloading).
func (e Env) CommonArgs() []string {
	s := e.Settings
	a := []string{"--ignore-config", "--encoding", "utf-8", "--color", "never", "--socket-timeout", "30"}
	if s.Proxy != "" {
		a = append(a, "--proxy="+s.Proxy)
	}
	if s.ForceIPv4 {
		// Browsers fall back to IPv4 at once when IPv6 doesn't connect; yt-dlp waits out its
		// socket timeout on every address, and every request crawls.
		a = append(a, "--force-ipv4")
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
	if e.PluginDir != "" {
		a = append(a, "--plugin-dirs="+e.PluginDir) // added to the default folders, not instead
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
	// InfoFile is a recent analysis of URL (InfoCache.Take). When set, yt-dlp downloads from it
	// instead of reading the page again.
	InfoFile string
}

// WaitInterval is --wait-for-video's minimum, in seconds: how often a stream that is past its
// scheduled time (or has none) is looked at again.
const WaitInterval = "60"

// thumbnailFits reports whether the output can hold an embedded thumbnail. WebM, WAV and raw AAC
// can't, and the post-processor would fail the whole download. (With no container chosen,
// yt-dlp itself switches video to MKV for this.)
func thumbnailFits(o settings.DownloadOptions) bool {
	switch {
	case o.Preset == "audio":
		return o.AudioFormat != "wav" && o.AudioFormat != "aac"
	case customAudio(o):
		return true // extracted as .opus, .ogg or .m4a
	}
	return o.Container != "webm"
}

// customAudio reports a format picked by hand that has no video.
func customAudio(o settings.DownloadOptions) bool {
	return o.Preset == "custom" && o.FormatAudioOnly
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
	out := OutputTemplate(s.FilenameTemplate, o, t)
	if len(o.Chapters) > 0 {
		out = SectionTemplate(s.FilenameTemplate, o, t)
	}
	a = append(a, "--paths="+dir, "--output="+out)
	a = append(a, "--trim-filenames=180")

	a = append(a, FormatArgs(o)...)
	a = append(a, SubtitleArgs(subtitlesFor(o))...)

	if o.EmbedMetadata {
		a = append(a, "--embed-metadata")
	}
	// --embed-metadata also embeds chapters and any .info.json unless told not to.
	if o.EmbedChapters {
		a = append(a, "--embed-chapters")
	} else if o.EmbedMetadata {
		a = append(a, "--no-embed-chapters")
	}
	if o.EmbedMetadata && o.WriteInfoJSON {
		a = append(a, "--no-embed-info-json")
	}
	if o.EmbedThumbnail && thumbnailFits(o) {
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
		if o.SponsorBlockMark {
			a = append(a, "--sponsorblock-mark="+strings.Join(o.SponsorBlock, ","))
		} else {
			a = append(a, "--sponsorblock-remove="+strings.Join(o.SponsorBlock, ","))
		}
	}
	if o.SplitChapters {
		a = append(a, "--split-chapters", "--output=chapter:"+ChapterTemplate(s.FilenameTemplate, o, t))
	}
	if o.LiveFromStart {
		a = append(a, "--live-from-start")
	}
	if o.WaitForVideo {
		// yt-dlp sleeps until the scheduled start, then looks again every minute until it's live.
		a = append(a, "--wait-for-video="+WaitInterval)
	}
	for _, c := range o.Chapters {
		// A chapter is picked by a regex on its title (yt-dlp matches with Python's re, which
		// reads Go's escapes the same way). Only that way does yt-dlp fill in section_title and
		// section_number for the file name; time ranges would all get the same name and only
		// the first would be kept. Two chapters with the same title both match.
		a = append(a, "--download-sections=^"+regexp.QuoteMeta(c)+"$")
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
	if s.SleepRequests > 0 {
		a = append(a, "--sleep-requests="+strconv.FormatFloat(s.SleepRequests, 'f', -1, 64))
	}
	if s.SleepInterval > 0 {
		a = append(a, "--sleep-interval="+strconv.FormatFloat(s.SleepInterval, 'f', -1, 64))
	}
	if s.UseArchive && e.ArchiveFile != "" && !o.IgnoreArchive {
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
	if t.InfoFile != "" {
		return append(a, "--load-info-json="+t.InfoFile)
	}
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
		a = append(a, "--format="+withAudioLang("ba/b", o.AudioLang), "--extract-audio", "--audio-format="+o.AudioFormat)
		switch o.AudioFormat {
		case "mp3", "aac", "opus", "m4a":
			if o.AudioQuality != "" {
				a = append(a, "--audio-quality="+o.AudioQuality)
			}
		}
		return a
	case "custom":
		a = append(a, "--format="+o.FormatID)
		if o.FormatAudioOnly {
			// Left alone, YouTube's Opus audio stays a .webm, which can't hold cover art. With no
			// --audio-format the stream is copied into its own type (.opus, .m4a), not re-encoded.
			return append(a, "--extract-audio")
		}
	case "best", "":
		a = append(a, "--format="+withAudioLang("bv*+ba/b", o.AudioLang), "--format-sort="+QualitySort)
	default:
		h := o.Preset
		// "<=?" also admits formats whose height is unknown (some sites don't report it).
		expr := fmt.Sprintf("bv*[height<=%[1]s]+ba/b[height<=%[1]s]/bv*[height<=?%[1]s]+ba/b[height<=?%[1]s]", h)
		a = append(a, "--format="+withAudioLang(expr, o.AudioLang), "--format-sort="+QualitySort)
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

// subtitlesFor leaves embedding out of audio downloads. yt-dlp embeds subtitles in .m4a as a
// mov_text stream, then its metadata step copies only the audio codec, and ffmpeg fails the
// download with "Encoder not found" (observed with ffmpeg 9). Players ignore subtitles in audio
// files anyway; "both" still saves them as files.
func subtitlesFor(o settings.DownloadOptions) settings.SubtitleOptions {
	s := o.Subtitles
	if o.Preset != "audio" && !customAudio(o) {
		return s
	}
	switch s.Mode {
	case "embed":
		s.Mode = "none"
	case "both":
		s.Mode = "files"
	}
	return s
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

// withAudioLang puts a try for the audio track in lang in front of a format expression: its first
// alternative with "ba" narrowed to that language, then the expression as it was, so formats
// without that language still download with the default track.
func withAudioLang(expr, lang string) string {
	if lang == "" {
		return expr
	}
	first, _, _ := strings.Cut(expr, "/")
	pick := "ba[language^=" + lang + "]"
	switch {
	case strings.Contains(first, "+ba"):
		first = strings.Replace(first, "+ba", "+"+pick, 1)
	case strings.HasPrefix(first, "ba"):
		first = pick + first[2:]
	default:
		return expr
	}
	return first + "/" + expr
}

// SectionTemplate names the files of a download of chosen chapters like ChapterTemplate does.
// For --download-sections, section_number counts from 0 (it is 1-based for --split-chapters).
func SectionTemplate(base string, o settings.DownloadOptions, t Target) string {
	return videoFolder(base, o, t) + "/%(section_number+1)02d - %(section_title)s.%(ext)s"
}

// ChapterTemplate names the files --split-chapters writes: a folder named like the video would
// be (the filename template, playlist folder and numbering) holding "01 - Chapter title.ext" files.
func ChapterTemplate(base string, o settings.DownloadOptions, t Target) string {
	return videoFolder(base, o, t) + "/%(section_number)02d - %(section_title)s.%(ext)s"
}

// videoFolder is the video's output template without its extension, used as a folder.
func videoFolder(base string, o settings.DownloadOptions, t Target) string {
	out := OutputTemplate(base, o, t)
	if strings.HasSuffix(out, ".%(ext)s") {
		return strings.TrimSuffix(out, ".%(ext)s")
	}
	return strings.TrimSpace(strings.Replace(out, "%(ext)s", "", 1))
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

// Environ is the environment a yt-dlp run gets.
func (e Env) Environ() []string {
	return append(childEnv(), e.Vars...)
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
