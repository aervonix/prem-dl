package ytdlp

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"yt-dlp-gui/internal/settings"
)

// ---- progress parsing: lines are taken from a real yt-dlp 2026.10.21 run ----

const (
	subLine    = `YTGUI_P {"status": "downloading", "downloaded_bytes": 3072, "total_bytes": null, "filename": "C:\\d\\Rick [x].en.vtt", "eta": null, "speed": 3070026.65}`
	videoLine  = `YTGUI_P {"status": "downloading", "downloaded_bytes": 2096128, "total_bytes": 5685561, "filename": "C:\\d\\Rick [x].f396.mp4", "eta": 0, "speed": 40386965.1}`
	videoDone  = `YTGUI_P {"downloaded_bytes": 5685561, "total_bytes": 5685561, "filename": "C:\\d\\Rick [x].f396.mp4", "status": "finished", "speed": 50163280.1}`
	audioStart = `YTGUI_P {"status": "downloading", "downloaded_bytes": 1024, "total_bytes": 3433755, "filename": "C:\\d\\Rick [x].f251.webm", "eta": null, "speed": null}`
	audioMid   = `YTGUI_P {"status": "downloading", "downloaded_bytes": 1716877, "total_bytes": 3433755, "filename": "C:\\d\\Rick [x].f251.webm", "eta": 1, "speed": 1000000}`
	streamsRaw = `YTGUI_STREAMS [{"filesize": 5685561, "filesize_approx": 5685558, "vcodec": "av01.0.01M.08", "acodec": "none"}, {"filesize": 3433755, "filesize_approx": 3433744, "vcodec": "none", "acodec": "opus"}]`
)

func TestParseLineKinds(t *testing.T) {
	if l := ParseLine(videoLine); l.Kind != KindProgress || l.Progress.Total != 5685561 || l.Progress.ETA != 0 {
		t.Fatalf("progress: %+v", l)
	}
	if l := ParseLine(audioStart); l.Progress.ETA != -1 || l.Progress.Speed != 0 {
		t.Fatalf("null eta/speed should be -1/0: %+v", l.Progress)
	}
	if l := ParseLine("YTGUI_PP Merger|started"); l.Kind != KindPost || l.PostName != "Merger" || l.PostStat != "started" {
		t.Fatalf("post: %+v", l)
	}
	if l := ParseLine("YTGUI_FILE\tC:\\d\\Rick [x].webm\r\n"); l.Kind != KindFile || l.File != `C:\d\Rick [x].webm` {
		t.Fatalf("file: %+v", l)
	}
	if l := ParseLine("YTGUI_FILE\tNA"); l.Kind != KindOther {
		t.Fatalf("NA path must be ignored: %+v", l)
	}
	if l := ParseLine("[download] Destination: x.mp4"); l.Kind != KindOther {
		t.Fatalf("ordinary log line: %+v", l)
	}
	if l := ParseLine("YTGUI_P {broken"); l.Kind != KindOther {
		t.Fatalf("malformed JSON must not crash or classify: %+v", l)
	}
	l := ParseLine(`YTGUI_PLAN {"id": "dQw4w9WgXcQ", "title": "Never Gonna Give You Up", "duration": 213, "filesize_approx": 9119316}`)
	if l.Kind != KindPlan || l.Plan.Title != "Never Gonna Give You Up" || l.Plan.Size() != 9119316 {
		t.Fatalf("plan: %+v", l)
	}
}

func TestParseWaitLines(t *testing.T) {
	// Captured from --wait-for-video on a stream due in three days.
	l := ParseLine("[wait] Waiting for 77:19:39 - Press Ctrl+C to try now")
	if want := 77*time.Hour + 19*time.Minute + 39*time.Second; l.Kind != KindWait || l.Wait != want {
		t.Fatalf("wait: %+v", l)
	}
	if l := ParseLine("[wait] Remaining time until next attempt: 77:19:38"); l.Kind != KindWaitTick {
		t.Fatalf("countdown: %+v", l)
	}
	if l := ParseLine("[wait] Wait period ended"); l.Kind != KindOther {
		t.Fatalf("other [wait] lines are ordinary: %+v", l)
	}
}

func TestUpcomingVideo(t *testing.T) {
	// Shape of `yt-dlp -J --ignore-no-formats-error` on a scheduled stream (NASA, captured).
	res, err := ParseProbe([]byte(`{"_type":"video","id":"hv867HnY4j4","title":"U.S. Spacewalk 100","uploader":"NASA",
		"live_status":"is_upcoming","release_timestamp":1793270700,"is_live":false,"duration":null,"formats":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if v := res.Video; !v.Upcoming || v.ReleaseTimestamp != 1793270700 {
		t.Fatalf("%+v", v)
	}

	e := testEnv()
	o := e.Settings.Defaults
	if has(e.DownloadArgs(o, Target{URL: "https://example.com/v"}), "--wait-for-video="+WaitInterval) {
		t.Error("only a download that asks to wait passes --wait-for-video")
	}
	o.WaitForVideo = true
	if !has(e.DownloadArgs(o, Target{URL: "https://example.com/v"}), "--wait-for-video="+WaitInterval) {
		t.Error("--wait-for-video missing")
	}
}

func TestParseStreams(t *testing.T) {
	got := ParseLine(streamsRaw).Streams
	want := []Stream{{Size: 5685561, Kind: "video"}, {Size: 3433755, Kind: "audio"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if s := ParseLine("YTGUI_STREAMS NA").Streams; s != nil {
		t.Fatalf("NA should give no streams, got %+v", s)
	}
}

func TestTrackerTwoStreamsGivesOneMonotonicBar(t *testing.T) {
	var tr Tracker
	tr.SetStreams(ParseLine(streamsRaw).Streams)

	// Subtitle progress arrives first and must not count as a stream.
	if _, ok := tr.Update(ParseLine(subLine).Progress); ok {
		t.Fatal("subtitle file progress should be ignored")
	}

	total := float64(5685561 + 3433755)
	snap, ok := tr.Update(ParseLine(videoLine).Progress)
	if !ok || snap.Stage != "Downloading video" {
		t.Fatalf("video: %+v ok=%v", snap, ok)
	}
	wantPct := 2096128 / total * 100
	if d := snap.Percent - wantPct; d > 0.01 || d < -0.01 {
		t.Fatalf("percent = %.3f want %.3f", snap.Percent, wantPct)
	}
	prev := snap.Percent

	if tr.AllDone() {
		t.Fatal("not done while the first of two streams is mid-download")
	}
	snap, _ = tr.Update(ParseLine(videoDone).Progress)
	if snap.Percent < prev {
		t.Fatalf("went backwards at end of video: %.2f < %.2f", snap.Percent, prev)
	}
	if tr.AllDone() {
		t.Fatal("finishing the video stream must not count as all done — audio is still to come")
	}
	prev = snap.Percent

	// The audio stream restarts at 0 bytes; overall progress must not drop.
	snap, _ = tr.Update(ParseLine(audioStart).Progress)
	if snap.Stage != "Downloading audio" {
		t.Fatalf("stage = %q", snap.Stage)
	}
	if snap.Percent < prev {
		t.Fatalf("bar jumped back when audio began: %.2f < %.2f", snap.Percent, prev)
	}
	audioFinished := ParseLine(audioMid).Progress
	audioFinished.Status, audioFinished.Downloaded = "finished", 3433755
	snap, _ = tr.Update(ParseLine(audioMid).Progress)
	if tr.AllDone() {
		t.Fatal("audio is only half done")
	}
	tr2 := tr // a copy: check the end state without disturbing the assertions below
	if _, _ = tr2.Update(audioFinished); !tr2.AllDone() {
		t.Fatal("all streams finished should report done")
	}
	wantPct = (5685561 + 1716877) / total * 100
	if d := snap.Percent - wantPct; d > 0.01 || d < -0.01 {
		t.Fatalf("mid-audio percent = %.3f want %.3f", snap.Percent, wantPct)
	}
	if snap.ETA != 1 { // (3433755-1716877)/1000000 ≈ 1.7 → 1
		t.Fatalf("ETA should be recomputed from the overall remainder, got %d", snap.ETA)
	}
}

func TestTrackerWithoutKnownSizes(t *testing.T) {
	var tr Tracker
	snap, ok := tr.Update(ParseLine(audioStart).Progress)
	if !ok {
		t.Fatal("expected a snapshot")
	}
	// With no plan the current stream's own total is all we have.
	if snap.Total != 3433755 {
		t.Fatalf("total = %d", snap.Total)
	}
	var live Tracker
	snap, _ = live.Update(Progress{Status: "downloading", Downloaded: 500, Filename: "live.mp4"})
	if snap.Percent != -1 {
		t.Fatalf("unknown total must report -1, got %v", snap.Percent)
	}
}

func TestTrackerTimeRangeIsOneMergedFile(t *testing.T) {
	// Observed with --download-sections "*0:05-0:10" -f "bv*+ba": no per-stream files, a single
	// "finished" report for the final file, sized for the cut rather than the whole video.
	tr := Tracker{Cut: true}
	tr.SetPlan(Plan{Filesize: 9119316})
	tr.SetStreams(ParseLine(streamsRaw).Streams)
	snap, _ := tr.Update(Progress{Status: "finished", Downloaded: 364835, Total: 364835, Filename: `C:\dl\dQw4w9WgXcQ.webm`})
	if !tr.AllDone() {
		t.Fatal("the merged file finishing means everything is downloaded")
	}
	if snap.Percent != 100 || snap.Total != 364835 || snap.Stage != "Downloading" {
		t.Fatalf("want 100%% of the cut: %+v", snap)
	}
}

func TestTrackerChaptersAreOneShareEach(t *testing.T) {
	// Observed with two --download-sections chapters: each is its own file, reported 0-100%.
	tr := Tracker{Cut: true, Sections: 2}
	dir := `C:\dl\But what is a neural network [aircAruvnKk]\`
	steps := []struct {
		p    Progress
		want float64
		done bool
	}{
		{Progress{Status: "downloading", Downloaded: 768545, Total: 1537091, Filename: dir + "01 - Introduction example.webm"}, 25, false},
		{Progress{Status: "finished", Downloaded: 1537091, Total: 1537091, Filename: dir + "01 - Introduction example.webm"}, 50, false},
		{Progress{Status: "downloading", Downloaded: 690582, Total: 1381165, Filename: dir + "03 - What are neurons？.webm"}, 75, false},
		{Progress{Status: "finished", Downloaded: 1381165, Total: 1381165, Filename: dir + "03 - What are neurons？.webm"}, 100, true},
	}
	for i, s := range steps {
		snap, _ := tr.Update(s.p)
		if math.Abs(snap.Percent-s.want) > 0.1 || tr.AllDone() != s.done {
			t.Fatalf("step %d: %.1f%% done=%v, want %.0f%% done=%v", i, snap.Percent, tr.AllDone(), s.want, s.done)
		}
	}
	snap, _ := tr.Update(steps[3].p)
	if snap.Stage != "Downloading chapter 2 of 2" || snap.Total != 1537091+1381165 {
		t.Fatalf("%+v", snap)
	}
}

func TestChapterArgs(t *testing.T) {
	e := testEnv()
	o := e.Settings.Defaults
	o.Chapters = []string{"Introduction example", "What are neurons?"}
	args := e.DownloadArgs(o, Target{URL: "https://example.com/v"})
	for _, want := range []string{
		`--download-sections=^Introduction example$`,
		`--download-sections=^What are neurons\?$`,
		"--output=%(title)s [%(id)s]/%(section_number+1)02d - %(section_title)s.%(ext)s",
	} {
		if !has(args, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}

	res, err := ParseProbe([]byte(`{"_type":"video","id":"aircAruvnKk","title":"t","chapters":[
		{"start_time":0,"title":"Introduction example","end_time":67},{"start_time":67,"title":"Series preview","end_time":162}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if c := res.Video.Chapters; len(c) != 2 || c[1] != (Chapter{Title: "Series preview", Start: 67, End: 162}) {
		t.Fatalf("%+v", c)
	}
	if res, _ := ParseProbe([]byte(`{"_type":"video","id":"x","title":"t","chapters":null}`)); res.Video.Chapters == nil {
		t.Error("no chapters must be an empty list, not null")
	}
}

func TestTrackerTwoStreamsWithUnknownSizesNeverGoesBack(t *testing.T) {
	var tr Tracker
	tr.SetStreams([]Stream{{Kind: "video"}, {Kind: "audio"}})
	var prev float64
	for _, p := range []Progress{
		{Status: "downloading", Downloaded: 500, Total: 1000, Filename: "v.f137.mp4"},
		{Status: "finished", Downloaded: 1000, Total: 1000, Filename: "v.f137.mp4"},
		{Status: "downloading", Downloaded: 10, Total: 100, Filename: "v.f140.m4a"},
		{Status: "finished", Downloaded: 100, Total: 100, Filename: "v.f140.m4a"},
	} {
		snap, _ := tr.Update(p)
		if snap.Percent < prev {
			t.Fatalf("bar went back from %.1f to %.1f at %+v", prev, snap.Percent, p)
		}
		if snap.Percent >= 100 && !tr.AllDone() {
			t.Fatalf("full bar before the last stream finished: %+v", p)
		}
		prev = snap.Percent
	}
	if prev != 100 || !tr.AllDone() {
		t.Fatalf("end: %.1f done=%v", prev, tr.AllDone())
	}
}

func TestTrackerLowEstimateDoesNotShowFullBarEarly(t *testing.T) {
	var tr Tracker
	tr.SetStreams([]Stream{{Size: 1000, Kind: "video"}, {Size: 100, Kind: "audio"}})
	tr.Update(Progress{Status: "finished", Downloaded: 1500, Total: 1500, Filename: "v.f1.mp4"})
	snap, _ := tr.Update(Progress{Status: "downloading", Downloaded: 50, Total: 100, Speed: 10, Filename: "v.f2.m4a"})
	if snap.Percent >= 100 || snap.ETA < 0 {
		t.Fatalf("estimate ran low: want <100%% and ETA >= 0, got %+v", snap)
	}
}

func TestTrackerAlreadyDownloadedStreamCounts(t *testing.T) {
	var tr Tracker
	tr.SetStreams([]Stream{{Size: 900, Kind: "video"}, {Size: 100, Kind: "audio"}})
	// yt-dlp's "already downloaded" report carries the size but no downloaded_bytes.
	tr.Update(Progress{Status: "finished", Total: 900, Filename: "v.f1.mp4"})
	snap, _ := tr.Update(Progress{Status: "downloading", Downloaded: 50, Total: 100, Filename: "v.f2.m4a"})
	if snap.Downloaded != 950 {
		t.Fatalf("downloaded = %d, want 950", snap.Downloaded)
	}
}

// ---- argument building ----

func testEnv() Env {
	s := settings.Default()
	s.DownloadDir = `C:\Dl`
	return Env{Bin: "yt-dlp", Settings: s}
}

func has(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestDownloadArgsBasics(t *testing.T) {
	e := testEnv()
	o := e.Settings.Defaults
	args := e.DownloadArgs(o, Target{URL: "https://example.com/v"})

	for _, want := range []string{"--ignore-config", "--no-playlist", "--paths=C:\\Dl", "--format=bv*+ba/b", "--embed-metadata", "--embed-chapters"} {
		if !has(args, want) {
			t.Errorf("missing %q in %v", want, args)
		}
	}
	// The URL must be the last argument, preceded by "--" so it can never be read as an option.
	n := len(args)
	if args[n-1] != "https://example.com/v" || args[n-2] != "--" {
		t.Fatalf("URL placement wrong: %v", args[n-2:])
	}
	if has(args, "--write-subs") || has(args, "--embed-subs") {
		t.Error("subtitles are off by default")
	}
}

func TestNoThumbnailEmbedInWebM(t *testing.T) {
	// Observed: "Supported filetypes for thumbnail embedding are: mp3, mkv/mka, ogg/opus/flac,
	// m4a/mp4/m4v/mov" fails the download after the media is already on disk.
	e := testEnv()
	o := e.Settings.Defaults
	o.EmbedThumbnail, o.Container = true, "webm"
	if has(e.DownloadArgs(o, Target{URL: "https://example.com/v"}), "--embed-thumbnail") {
		t.Error("--embed-thumbnail must not be passed with WebM")
	}
	o.Container = ""
	if !has(e.DownloadArgs(o, Target{URL: "https://example.com/v"}), "--embed-thumbnail") {
		t.Error("with no container chosen yt-dlp handles it, so the flag stays")
	}
	o.Preset = "audio"
	for format, want := range map[string]bool{"mp3": true, "m4a": true, "flac": true, "opus": true, "wav": false, "aac": false} {
		o.AudioFormat = format
		if got := has(e.DownloadArgs(o, Target{URL: "https://example.com/v"}), "--embed-thumbnail"); got != want {
			t.Errorf("audio %s: --embed-thumbnail passed = %v, want %v", format, got, want)
		}
	}
}

func TestCustomAudioFormatIsExtracted(t *testing.T) {
	// Observed: format 774 (Opus in WebM) picked by hand with cover art on stayed a .webm and
	// failed with "Supported filetypes for thumbnail embedding are: ...".
	e := testEnv()
	o := e.Settings.Defaults
	o.Preset, o.FormatID, o.FormatAudioOnly = "custom", "774", true
	o.EmbedThumbnail, o.Container = true, "mp4"
	o.Subtitles = settings.SubtitleOptions{Mode: "embed", Langs: []string{"en"}}
	args := e.DownloadArgs(o, Target{URL: "https://example.com/v"})
	for _, want := range []string{"--format=774", "--extract-audio", "--embed-thumbnail"} {
		if !has(args, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, unwanted := range []string{"--audio-format=mp3", "--remux-video=mp4", "--merge-output-format=mp4", "--embed-subs"} {
		if has(args, unwanted) {
			t.Errorf("%s must not be passed for an audio-only format: %v", unwanted, args)
		}
	}

	// A format with video is left as it was.
	o.FormatAudioOnly = false
	if args := e.DownloadArgs(o, Target{URL: "https://example.com/v"}); has(args, "--extract-audio") || !has(args, "--remux-video=mp4") {
		t.Errorf("format with video: %v", args)
	}
}

func TestNoSubtitleEmbedInAudio(t *testing.T) {
	// Observed: --embed-subs into an .m4a, then --embed-metadata, fails with "Error opening output
	// files: Encoder not found" (the metadata step doesn't copy the mov_text stream).
	e := testEnv()
	o := e.Settings.Defaults
	o.Preset, o.AudioFormat = "audio", "m4a"
	o.Subtitles = settings.SubtitleOptions{Mode: "embed", Langs: []string{"en"}}
	args := e.DownloadArgs(o, Target{URL: "https://example.com/v"})
	if has(args, "--embed-subs") || has(args, "--write-subs") || has(args, "--sub-langs=en") {
		t.Errorf("embed-only subtitles on an audio download should download nothing: %v", args)
	}
	o.Subtitles.Mode = "both"
	args = e.DownloadArgs(o, Target{URL: "https://example.com/v"})
	if has(args, "--embed-subs") || !has(args, "--write-subs") {
		t.Errorf("'both' on an audio download should still save the files: %v", args)
	}
	o.Preset, o.Subtitles.Mode = "best", "embed"
	if !has(e.DownloadArgs(o, Target{URL: "https://example.com/v"}), "--embed-subs") {
		t.Error("video downloads still embed subtitles")
	}
}

func TestAudioLanguage(t *testing.T) {
	// Each was checked against yt-dlp on a dubbed video (0e3GPea1Tyg): "de" picks 251-2 (German)
	// and an unknown language falls back to the default track.
	cases := map[string]string{
		"best":  "--format=bv*+ba[language^=de]/bv*+ba/b",
		"720":   "--format=bv*[height<=720]+ba[language^=de]/bv*[height<=720]+ba/b[height<=720]/bv*[height<=?720]+ba/b[height<=?720]",
		"audio": "--format=ba[language^=de]/ba/b",
	}
	for preset, want := range cases {
		o := settings.Default().Defaults
		o.Preset, o.AudioLang = preset, "de"
		if !has(FormatArgs(o), want) {
			t.Errorf("%s: %v", preset, FormatArgs(o))
		}
	}
	o := settings.Default().Defaults
	o.Preset, o.FormatID, o.AudioLang = "custom", "137+140", "de"
	if !has(FormatArgs(o), "--format=137+140") {
		t.Errorf("a picked format is left alone: %v", FormatArgs(o))
	}

	res, err := ParseProbe([]byte(`{"_type":"video","id":"x","title":"t",
		"requested_formats":[{"format_id":"616","vcodec":"vp09","acodec":"none"},{"format_id":"251-23","vcodec":"none","acodec":"opus","language":"en-US"}],
		"formats":[{"format_id":"251-2","vcodec":"none","acodec":"opus","language":"de","format_note":"German, low"},
			{"format_id":"251-23","vcodec":"none","acodec":"opus","language":"en-US","format_note":"English (United States) original (default)"},
			{"format_id":"251-3","vcodec":"none","acodec":"opus","language":"zh-Hans"},
			{"format_id":"616","vcodec":"vp09","acodec":"none","dynamic_range":"SDR"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Video.AudioLanguages, ","); got != "en-US,de,zh-Hans" {
		t.Errorf("languages %q, want the default first", got)
	}
}

func TestMoreDownloadOptions(t *testing.T) {
	e := testEnv()
	o := e.Settings.Defaults
	target := Target{URL: "https://example.com/v"}
	o.SponsorBlock = []string{"sponsor", "intro"}
	if args := e.DownloadArgs(o, target); !has(args, "--sponsorblock-remove=sponsor,intro") {
		t.Errorf("cutting is the default: %v", args)
	}
	o.SponsorBlockMark = true
	if args := e.DownloadArgs(o, target); !has(args, "--sponsorblock-mark=sponsor,intro") || has(args, "--sponsorblock-remove=sponsor,intro") {
		t.Errorf("marking replaces cutting: %v", args)
	}

	o.SplitChapters, o.LiveFromStart = true, true
	args := e.DownloadArgs(o, Target{URL: "https://example.com/v", PlaylistTitle: "My: List", PlaylistIndex: 2, PlaylistCount: 9})
	if !has(args, "--split-chapters") || !has(args, "--output=chapter:My_ List/%(title)s [%(id)s]/%(section_number)02d - %(section_title)s.%(ext)s") {
		t.Errorf("chapters go into a folder per video inside the playlist folder: %v", args)
	}
	if !has(args, "--live-from-start") {
		t.Error("--live-from-start missing")
	}

	e.Settings.SleepRequests, e.Settings.SleepInterval = 1.5, 10
	args = e.DownloadArgs(e.Settings.Defaults, target)
	if !has(args, "--sleep-requests=1.5") || !has(args, "--sleep-interval=10") {
		t.Errorf("sleep flags: %v", args)
	}
}

func TestIgnoreArchive(t *testing.T) {
	e := testEnv()
	e.Settings.UseArchive = true
	e.ArchiveFile = `C:\data\archive.txt`
	o := e.Settings.Defaults
	if !has(e.DownloadArgs(o, Target{URL: "https://example.com/v"}), `--download-archive=C:\data\archive.txt`) {
		t.Fatal("the archive should be used when remembering downloads")
	}
	o.IgnoreArchive = true
	for _, a := range e.DownloadArgs(o, Target{URL: "https://example.com/v"}) {
		if strings.HasPrefix(a, "--download-archive") {
			t.Fatalf("\"download again\" must not pass the archive: %s", a)
		}
	}
}

func TestChaptersOffWithMetadataOn(t *testing.T) {
	// --embed-metadata embeds chapters (and an .info.json) by itself unless told not to.
	e := testEnv()
	o := e.Settings.Defaults
	o.EmbedMetadata, o.EmbedChapters, o.WriteInfoJSON = true, false, true
	args := e.DownloadArgs(o, Target{URL: "https://example.com/v"})
	if !has(args, "--no-embed-chapters") || has(args, "--embed-chapters") {
		t.Errorf("chapters were switched off: %v", args)
	}
	if !has(args, "--no-embed-info-json") {
		t.Errorf("the .info.json should stay a separate file: %v", args)
	}
	o.EmbedMetadata = false
	if args := e.DownloadArgs(o, Target{URL: "https://example.com/v"}); has(args, "--no-embed-chapters") || has(args, "--no-embed-info-json") {
		t.Errorf("nothing to switch off without --embed-metadata: %v", args)
	}
}

func TestFormatPresets(t *testing.T) {
	cases := []struct {
		name string
		o    settings.DownloadOptions
		want []string
	}{
		{"720", settings.DownloadOptions{Preset: "720"},
			[]string{"--format=bv*[height<=720]+ba/b[height<=720]/bv*[height<=?720]+ba/b[height<=?720]", "--format-sort=res,fps,hdr:12,vbr"}},
		// Observed (ulLBRyVrI1Y, signed in): default ranking picked the 0.4 Mbps AV1 "1080p Premium"
		// stream over the 2.8 Mbps VP9 one. Bitrate now outranks codec at equal resolution.
		{"best ranks bitrate above codec", settings.DownloadOptions{Preset: "best"},
			[]string{"--format=bv*+ba/b", "--format-sort=res,fps,hdr:12,vbr"}},
		{"audio mp3", settings.DownloadOptions{Preset: "audio", AudioFormat: "mp3", AudioQuality: "0"},
			[]string{"--format=ba/b", "--extract-audio", "--audio-format=mp3", "--audio-quality=0"}},
		{"audio flac has no quality", settings.DownloadOptions{Preset: "audio", AudioFormat: "flac", AudioQuality: "0"},
			[]string{"--format=ba/b", "--extract-audio", "--audio-format=flac"}},
		{"custom with container", settings.DownloadOptions{Preset: "custom", FormatID: "137+140", Container: "mp4"},
			[]string{"--format=137+140", "--merge-output-format=mp4", "--remux-video=mp4"}},
		// Observed: h264 into WebM fails in ffmpeg; with the fallback it becomes .mkv, VP9 stays .webm.
		{"webm falls back to mkv", settings.DownloadOptions{Preset: "best", Container: "webm"},
			[]string{"--format=bv*+ba/b", "--format-sort=res,fps,hdr:12,vbr", "--merge-output-format=webm/mkv", "--remux-video=webm>webm/mkv"}},
	}
	for _, c := range cases {
		got := FormatArgs(c.o)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestSubtitleModes(t *testing.T) {
	langs := []string{"en", "es"}
	cases := []struct {
		name string
		o    settings.SubtitleOptions
		want []string
	}{
		{"none", settings.SubtitleOptions{Mode: "none", Langs: langs}, nil},
		{"files", settings.SubtitleOptions{Mode: "files", Langs: langs}, []string{"--sub-langs=en,es", "--write-subs"}},
		// Embed-only must not pass --write-subs or yt-dlp keeps the sidecar files (observed).
		{"embed", settings.SubtitleOptions{Mode: "embed", Langs: langs}, []string{"--sub-langs=en,es", "--embed-subs"}},
		// Observed: --embed-subs --write-auto-subs alone picks the auto captions over uploaded
		// subtitles; --write-subs fixes that and no-keep-subs still removes the sidecars.
		{"embed+auto", settings.SubtitleOptions{Mode: "embed", Langs: langs, Auto: true},
			[]string{"--sub-langs=en,es", "--write-subs", "--compat-options=no-keep-subs", "--embed-subs", "--write-auto-subs"}},
		{"both+auto+srt", settings.SubtitleOptions{Mode: "both", Langs: langs, Auto: true, Format: "srt"},
			[]string{"--sub-langs=en,es", "--write-subs", "--embed-subs", "--write-auto-subs", "--convert-subs=srt"}},
	}
	for _, c := range cases {
		if got := SubtitleArgs(c.o); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestOutputTemplate(t *testing.T) {
	o := settings.DownloadOptions{PlaylistFolder: true, PlaylistNumbering: true}
	base := settings.DefaultTemplate

	if got := OutputTemplate(base, o, Target{}); got != base {
		t.Errorf("non-playlist should keep the template, got %q", got)
	}
	tg := Target{PlaylistTitle: "My Mix: 50% <best>?", PlaylistIndex: 7, PlaylistCount: 120}
	want := "My Mix_ 50%% _best__/007 - " + base
	if got := OutputTemplate(base, o, tg); got != want {
		t.Errorf("got %q want %q", got, want)
	}
	o.PlaylistFolder = false
	if got := OutputTemplate(base, o, tg); got != "007 - "+base {
		t.Errorf("numbering only: %q", got)
	}
	o.PlaylistNumbering = false
	if got := OutputTemplate(base, o, tg); got != base {
		t.Errorf("neither: %q", got)
	}
	if got := sanitizeSegment("CON"); got != "CON_" {
		t.Errorf("reserved name: %q", got)
	}
	if got := sanitizeSegment("..."); got != "playlist" {
		t.Errorf("all-dots: %q", got)
	}
}

func TestPercentInTitleCannotInjectTemplateFields(t *testing.T) {
	// A playlist literally named "%(title)s" must stay literal text, not expand to the video title.
	got := sanitizeSegment("%(title)s")
	if got != "%%(title)s" {
		t.Fatalf("got %q", got)
	}
}

func TestCookieArgs(t *testing.T) {
	e := testEnv()
	e.Settings.Cookies = settings.Cookies{Mode: "browser", Browser: "firefox", Profile: "default-release"}
	if !has(e.CommonArgs(), "--cookies-from-browser=firefox:default-release") {
		t.Errorf("browser: %v", e.CommonArgs())
	}
	e.Settings.Cookies = settings.Cookies{Mode: "file", File: `C:\orig.txt`}
	e.CookieFile = `C:\tmp\private.txt`
	if !has(e.CommonArgs(), `--cookies=C:\tmp\private.txt`) {
		t.Errorf("file should use the private copy: %v", e.CommonArgs())
	}
	e.Settings.Cookies.Mode = "none"
	for _, a := range e.CommonArgs() {
		if strings.Contains(a, "cookies") {
			t.Errorf("no cookie flags expected, got %q", a)
		}
	}
}

func TestExtraArgsComeLast(t *testing.T) {
	e := testEnv()
	e.Settings.ExtraArgs = `--extractor-args "youtube:player_client=web" --no-mtime`
	args := e.DownloadArgs(e.Settings.Defaults, Target{URL: "https://x.test/"})
	n := len(args)
	if args[n-2] != "--" {
		t.Fatalf("separator missing: %v", args[n-3:])
	}
	if args[n-3] != "--no-mtime" || args[n-4] != "youtube:player_client=web" {
		t.Fatalf("extra args should directly precede --: %v", args[n-5:])
	}
}

func TestSplitArgs(t *testing.T) {
	got, err := SplitArgs(`--a "b c"  --d='e f' g\h`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--a", "b c", "--d=e f", `g\h`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err := SplitArgs(`--a "unterminated`); err == nil {
		t.Fatal("expected an error for an unterminated quote")
	}
	if got, _ := SplitArgs("   "); len(got) != 0 {
		t.Fatalf("blank input: %q", got)
	}
	if got, _ := SplitArgs(`--a ""`); !reflect.DeepEqual(got, []string{"--a", ""}) {
		t.Fatalf("empty quoted arg should survive: %q", got)
	}
}

// ---- url normalisation ----

func TestNormalizeURL(t *testing.T) {
	good := map[string]string{
		"https://youtu.be/abc":       "https://youtu.be/abc",
		"  youtube.com/watch?v=abc ": "https://youtube.com/watch?v=abc",
		"http://example.com/a b":     "", // space inside → rejected below
	}
	for in, want := range good {
		got, err := NormalizeURL(in)
		if want == "" {
			if err == nil {
				t.Errorf("%q should be rejected", in)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q → %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "--exec calc", "-o", "notalink", "ftp://x.test/f", "javascript:alert(1)", "file:///C:/x"} {
		if _, err := NormalizeURL(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

// ---- probe parsing ----

const singleVideoJSON = `{
 "_type": "video", "id": "dQw4w9WgXcQ", "title": "Never Gonna Give You Up", "webpage_url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
 "uploader": "Rick Astley", "duration": 213, "thumbnail": "https://i.ytimg.com/vi/x/max.webp", "extractor_key": "Youtube",
 "formats": [
  {"format_id":"sb0","ext":"mhtml","vcodec":"none","acodec":"none","format_note":"storyboard","height":27},
  {"format_id":"140","ext":"m4a","vcodec":"none","acodec":"mp4a.40.2","filesize":3400000,"tbr":129},
  {"format_id":"137","ext":"mp4","vcodec":"avc1","acodec":"none","height":1080,"width":1920,"fps":30,"filesize_approx":50000000},
  {"format_id":"18","ext":"mp4","vcodec":"avc1","acodec":"mp4a","height":360,"width":640},
  {"format_id":"248","ext":"webm","vcodec":"vp9","acodec":"none","height":1080,"width":1920}
 ],
 "subtitles": {"en": [{"name":"English","ext":"vtt"}], "live_chat": [{"ext":"json"}]},
 "automatic_captions": {"fr": [{"name":"French","ext":"vtt"}], "de": [{"ext":"vtt"}]}
}`

func TestParseSingleVideo(t *testing.T) {
	res, err := ParseProbe([]byte(singleVideoJSON))
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "video" || res.Video == nil {
		t.Fatalf("kind = %q", res.Kind)
	}
	v := res.Video
	if len(v.Formats) != 4 {
		t.Errorf("storyboard must be filtered out; got %d formats", len(v.Formats))
	}
	if !reflect.DeepEqual(v.Heights, []int{1080, 360}) {
		t.Errorf("heights = %v (want distinct, descending)", v.Heights)
	}
	for _, f := range v.Formats {
		if f.ID == "137" && (f.Filesize != 50000000 || !f.HasVideo || f.HasAudio) {
			t.Errorf("137 parsed wrong: %+v", f)
		}
		if f.ID == "140" && (f.HasVideo || !f.HasAudio) {
			t.Errorf("140 parsed wrong: %+v", f)
		}
	}
	if len(v.Subtitles) != 1 || v.Subtitles[0].Lang != "en" || v.Subtitles[0].Name != "English" {
		t.Errorf("subtitles (live_chat must be excluded): %+v", v.Subtitles)
	}
	if len(v.AutoCaptions) != 2 || v.AutoCaptions[0].Lang != "de" || v.AutoCaptions[0].Name != "de" || !v.AutoCaptions[0].Auto {
		t.Errorf("auto captions: %+v", v.AutoCaptions)
	}
}

const playlistJSON = `{
 "_type": "playlist", "id": "PL1", "title": "Channel Uploads", "webpage_url": "https://www.youtube.com/playlist?list=PL1",
 "entries": [
  {"_type":"url","id":"a","title":"First","url":"https://www.youtube.com/watch?v=a","duration":60,"channel":"Chan",
   "thumbnails":[{"url":"https://t/small.jpg","width":120},{"url":"https://t/mid.jpg","width":320},{"url":"https://t/big.jpg","width":1280}]},
  null,
  {"_type":"url","id":"b","title":"[Private video]","url":"https://www.youtube.com/watch?v=b"},
  {"_type":"playlist","title":"Shorts","entries":[
     {"_type":"url","id":"c","title":"Short one","url":"https://www.youtube.com/shorts/c"}
  ]}
 ]
}`

func TestParsePlaylist(t *testing.T) {
	res, err := ParseProbe([]byte(playlistJSON))
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "playlist" {
		t.Fatalf("kind = %q", res.Kind)
	}
	e := res.Playlist.Entries
	if len(e) != 3 {
		t.Fatalf("null entries are skipped and nested playlists flattened; got %d: %+v", len(e), e)
	}
	if e[0].Thumbnail != "https://t/mid.jpg" {
		t.Errorf("thumbnail should prefer the smallest ≥240px, got %q", e[0].Thumbnail)
	}
	if e[0].Uploader != "Chan" || e[0].Index != 1 || e[2].Index != 3 {
		t.Errorf("indexes/uploader: %+v", e)
	}
	if !e[1].Unavailable {
		t.Errorf("private video should be flagged unavailable")
	}
	if e[2].Group != "Shorts" || e[2].Unavailable {
		t.Errorf("nested entry: %+v", e[2])
	}
}

// Trimmed from a real probe of https://www.youtube.com/@RickAstleyYT/playlists.
const playlistsTabJSON = `{"_type":"playlist","id":"UCuAXFkgsw1L7xaCfnd5JJOw","title":"Rick Astley - Playlists","extractor_key":"YoutubeTab","entries":[
 {"title":"Rick Astley Live at The O2","duration":null,"view_count":null,"channel":"Playlist","uploader":"Playlist","ie_key":"YoutubeTab","id":"PLlaN88a7y2_qHDbY9eQbuNTAuEJUSEeuu","_type":"url","url":"https://www.youtube.com/playlist?list=PLlaN88a7y2_qHDbY9eQbuNTAuEJUSEeuu"},
 {"title":"A video","ie_key":"Youtube","id":"dQw4w9WgXcQ","_type":"url","url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
 {"title":"Video in a list","ie_key":"Youtube","id":"lW1aFyk_E5I","_type":"url","url":"https://www.youtube.com/watch?v=lW1aFyk_E5I&list=PLx"}
]}`

func TestNestedPlaylistEntriesAreFlagged(t *testing.T) {
	res, err := ParseProbe([]byte(playlistsTabJSON))
	if err != nil {
		t.Fatal(err)
	}
	e := res.Playlist.Entries
	if len(e) != 3 || !e[0].IsPlaylist || e[1].IsPlaylist || e[2].IsPlaylist {
		t.Fatalf("only the playlist link should be flagged: %+v", e)
	}
	if e[0].Unavailable {
		t.Error("a nested playlist is not unavailable")
	}
}

func TestProbeToleratesFractionalNumbers(t *testing.T) {
	res, err := ParseProbe([]byte(`{"_type":"video","id":"x","title":"t","view_count":12.0,
		"formats":[{"format_id":"a","ext":"mp4","vcodec":"avc1","acodec":"mp4a","height":720.0,"width":1280.0,"filesize_approx":1234.5}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if f := res.Video.Formats[0]; f.Height != 720 || f.Filesize != 1234 || res.Video.ViewCount != 12 {
		t.Fatalf("%+v views=%d", f, res.Video.ViewCount)
	}
}

func TestProbeToleratesMistypedFields(t *testing.T) {
	// One field of an unexpected type must not lose the rest of the answer or a playlist entry.
	res, err := ParseProbe([]byte(`{"_type":"video","id":12345,"title":"t","duration":10,
		"thumbnails":[{"url":"https://i/a.jpg","width":1280.0}],"formats":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Video.Title != "t" || res.Video.Duration != 10 || res.Video.Thumbnail != "https://i/a.jpg" {
		t.Fatalf("%+v", res.Video)
	}
	res, err = ParseProbe([]byte(`{"_type":"playlist","title":"p","entries":[
		{"_type":"url","id":1,"title":"one","url":"https://example.com/1"},
		{"_type":"url","id":"2","title":"two","url":"https://example.com/2"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res.Playlist.Entries); n != 2 {
		t.Fatalf("got %d entries, want 2", n)
	}
}

func TestThumbnailSizes(t *testing.T) {
	// Shape of a real YouTube answer (dQw4w9WgXcQ, trimmed): "thumbnail" is the 1920px WebP,
	// which costs megabytes per queue row once decoded.
	res, err := ParseProbe([]byte(`{"_type":"video","id":"x","title":"t",
		"thumbnail":"https://i/maxresdefault.webp",
		"thumbnails":[{"url":"https://i/3.jpg"},
			{"url":"https://i/mqdefault.jpg","width":320,"height":180},
			{"url":"https://i/hqdefault-168.jpg","width":168},{"url":"https://i/hqdefault-246.jpg","width":246},
			{"url":"https://i/hq720.jpg","width":1280},{"url":"https://i/maxresdefault.webp","width":1920}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if v := res.Video; v.Thumbnail != "https://i/hq720.jpg" || v.ThumbnailSmall != "https://i/hqdefault-246.jpg" {
		t.Fatalf("large %q, small %q", v.Thumbnail, v.ThumbnailSmall)
	}
	// Nothing listed is big enough: yt-dlp's own choice beats a tiny one.
	res, _ = ParseProbe([]byte(`{"_type":"video","id":"x","title":"t","thumbnail":"https://i/best.jpg",
		"thumbnails":[{"url":"https://i/small.jpg","width":120}]}`))
	if res.Video.Thumbnail != "https://i/best.jpg" {
		t.Fatalf("fallback %q", res.Video.Thumbnail)
	}
}

func TestSplitProbeOutput(t *testing.T) {
	// As printed by -J with --print=video:YTGUI_NAME %(filename)s (observed with yt-dlp 2026.10.21).
	out := "YTGUI_NAME Rick Astley - Never Gonna Give You Up [dQw4w9WgXcQ].webm\r\n{\"_type\":\"video\",\"id\":\"dQw4w9WgXcQ\",\"title\":\"t\"}\n"
	data, name := splitProbeOutput([]byte(out))
	if name != "Rick Astley - Never Gonna Give You Up [dQw4w9WgXcQ].webm" {
		t.Fatalf("name %q", name)
	}
	if res, err := ParseProbe(data); err != nil || res.Video.ID != "dQw4w9WgXcQ" {
		t.Fatalf("json after the name line: %v %+v", err, res)
	}
	// A playlist prints no name; the output is all JSON.
	if data, name := splitProbeOutput([]byte(`{"_type":"playlist"}`)); name != "" || string(data) != `{"_type":"playlist"}` {
		t.Fatalf("%q %q", data, name)
	}
}

func TestParseProbeRejectsGarbage(t *testing.T) {
	if _, err := ParseProbe([]byte("not json")); err == nil {
		t.Fatal("expected an error")
	}
}

// ---- cookies ----

func TestInspectCountsEmptyValueCookies(t *testing.T) {
	// A cookie with an empty value ends in a tab; trimming it left six fields and skipped it.
	data := "# Netscape HTTP Cookie File\r\n.youtube.com\tTRUE\t/\tTRUE\t0\tPREF\t\r\n"
	if info := inspect([]byte(data), time.Unix(2_000_000_000, 0)); info.Count != 1 || info.Problem != "" {
		t.Fatalf("%+v", info)
	}
}

func TestInspectCookies(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	good := "# Netscape HTTP Cookie File\r\n\r\n" +
		".youtube.com\tTRUE\t/\tTRUE\t2100000000\tSID\tsecret1\r\n" +
		"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t0\tHSID\tsecret2\r\n" +
		".google.com\tTRUE\t/\tFALSE\t1000000000\tOLD\tsecret3\r\n"
	info := inspect([]byte("\xef\xbb\xbf"+good), now)
	if !info.Valid || info.Count != 3 || info.Expired != 1 || info.Session != 1 || info.DomainCount != 2 {
		t.Fatalf("info: %+v", info)
	}
	if info.Domains[0] != "youtube.com" {
		t.Errorf("domains should be ordered by cookie count: %v", info.Domains)
	}
	if b, _ := jsonString(info); strings.Contains(b, "secret") {
		t.Fatal("cookie values must never appear in CookieInfo")
	}

	bad := map[string]string{
		"empty":     "",
		"json":      `[{"domain":".x.com"}]`,
		"no header": ".x.com\tTRUE\t/\tTRUE\t0\tA\tB\n",
		"no rows":   "# Netscape HTTP Cookie File\n# nothing\n",
		"spaces":    "# Netscape HTTP Cookie File\n.x.com TRUE / TRUE 0 A B\n",
	}
	for name, content := range bad {
		if got := inspect([]byte(content), now); got.Valid || got.Problem == "" {
			t.Errorf("%s should be invalid with a reason: %+v", name, got)
		}
	}
}

func TestImportCookiesNormalisesAndStores(t *testing.T) {
	dir := t.TempDir()
	in := "\xef\xbb\xbf# Netscape HTTP Cookie File\r\n.a.com\tTRUE\t/\tFALSE\t0\tX\tY\r\n"
	info, err := ImportCookies([]byte(in), dir)
	if err != nil || !info.Valid {
		t.Fatalf("%v %+v", err, info)
	}
	got := InspectCookieFile(info.Path)
	if !got.Valid || got.Count != 1 {
		t.Fatalf("re-read: %+v", got)
	}
	if _, err := ImportCookies([]byte("garbage"), dir); err == nil {
		t.Fatal("garbage must be rejected")
	}
	if missing := InspectCookieFile(dir + "/nope.txt"); missing.Valid || missing.Problem == "" {
		t.Fatalf("missing file: %+v", missing)
	}
}

func TestPrivateCookieCopyIsIndependent(t *testing.T) {
	dir := t.TempDir()
	info, _ := ImportCookies([]byte("# Netscape HTTP Cookie File\n.a.com\tTRUE\t/\tFALSE\t0\tX\tY\n"), dir)
	c1, done1, err := PrivateCookieCopy(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	c2, done2, _ := PrivateCookieCopy(info.Path)
	if c1 == c2 || c1 == info.Path {
		t.Fatalf("each run needs its own copy: %q %q %q", c1, c2, info.Path)
	}
	done1()
	done2()
	if InspectCookieFile(c1).Valid {
		t.Fatal("cleanup should delete the copy")
	}
}

// ---- versions ----

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2026.10.21", "2026.10.21", 0},
		{"2026.10.21", "2026.10.25", -1},
		{"2026.11.01", "2026.10.25", 1},
		{"2026.10.25.232749", "2026.10.25", 1}, // nightly after the stable of the same day
		{"2026.10.25.010101", "2026.10.25.232749", -1},
		{"2026.9.30", "2026.10.1", -1}, // numeric, not lexicographic
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	sums := []byte("AAA111  yt-dlp\nBBB222  yt-dlp.exe\nccc333 *yt-dlp_x86.exe\n")
	if h, ok := parseChecksum(sums, "yt-dlp.exe"); !ok || h != "bbb222" {
		t.Fatalf("got %q %v", h, ok)
	}
	if h, ok := parseChecksum(sums, "yt-dlp_x86.exe"); !ok || h != "ccc333" {
		t.Fatalf("binary-mode marker: %q %v", h, ok)
	}
	if _, ok := parseChecksum(sums, "missing.exe"); ok {
		t.Fatal("should not find a missing asset")
	}
}

func TestPostStage(t *testing.T) {
	if PostStage("Merger") != "Merging video and audio" || PostStage("SomethingNew") != "Processing" {
		t.Fatal("stage names")
	}
}

func TestBestPremiumComesFromTheDefaultChoice(t *testing.T) {
	// Trimmed from real probes: r6Rp-uo6HmI picks 616 "Premium"; dQw4w9WgXcQ picks 401 "2160p".
	cases := map[string]bool{
		`{"_type":"video","id":"p","format_id":"616+251","format_note":"Premium+medium","requested_formats":[{"format_id":"616","format_note":"Premium","vcodec":"vp09.00.40.08","acodec":"none"},{"format_id":"251","format_note":"medium","vcodec":"none","acodec":"opus"}]}`: true,
		`{"_type":"video","id":"n","format_id":"401+251","requested_formats":[{"format_id":"401","format_note":"2160p","vcodec":"av01","acodec":"none"},{"format_id":"251","format_note":"medium","vcodec":"none","acodec":"opus"}]}`:                                           false,
		`{"_type":"video","id":"s","format_id":"18","format_note":"360p"}`: false,
	}
	for raw, want := range cases {
		res, err := ParseProbe([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if res.Video.BestPremium != want {
			t.Errorf("%s: bestPremium = %v, want %v", res.Video.ID, res.Video.BestPremium, want)
		}
	}
}

func TestTagFromLocation(t *testing.T) {
	cases := map[string]string{
		"https://github.com/yt-dlp/yt-dlp/releases/tag/2026.10.21":                       "2026.10.21",
		"https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/tag/2026.10.25.232749": "2026.10.25.232749",
		"https://github.com/yt-dlp/yt-dlp/releases":                                      "",
		"https://github.com/yt-dlp/yt-dlp/releases/tag/../../evil/x":                     "",
		"": "",
	}
	for loc, want := range cases {
		if got := tagFromLocation(loc); got != want {
			t.Errorf("%q: got %q, want %q", loc, got, want)
		}
	}
}

func TestUpdateAvailable(t *testing.T) {
	cases := []struct {
		current, latest, channel string
		want                     bool
	}{
		{"", "2026.10.21", "stable", true},
		{"2026.09.01", "2026.10.21", "stable", true},
		{"2026.10.21", "2026.10.21", "stable", false},
		// Back on stable after a nightly: the nightly compares newer but stable is still on offer.
		{"2026.10.25.232749", "2026.10.21", "stable", true},
		{"2026.10.25.232749", "2026.10.24.101010", "nightly", false},
		{"2026.10.21", "2026.10.25.232749", "nightly", true},
	}
	for _, c := range cases {
		if got := UpdateAvailable(c.current, c.latest, c.channel); got != c.want {
			t.Errorf("%+v: got %v", c, got)
		}
	}
}

func TestYoutubeChannel(t *testing.T) {
	cases := []struct {
		link, home, tab string
		ok              bool
	}{
		{"https://www.youtube.com/@RickAstleyYT", "https://www.youtube.com/@RickAstleyYT", "", true},
		{"https://youtube.com/@RickAstleyYT/videos", "https://www.youtube.com/@RickAstleyYT", "videos", true},
		{"https://m.youtube.com/@x/Shorts?view=0", "https://www.youtube.com/@x", "shorts", true},
		{"https://www.youtube.com/channel/UCuAXFkgsw1L7xaCfnd5JJOw/streams", "https://www.youtube.com/channel/UCuAXFkgsw1L7xaCfnd5JJOw", "streams", true},
		{"https://www.youtube.com/c/name", "https://www.youtube.com/c/name", "", true},
		{"https://www.youtube.com/user/name/playlists", "https://www.youtube.com/user/name", "playlists", true},
		{"https://www.youtube.com/playlist?list=PL1", "", "", false},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "", "", false},
		{"https://www.youtube.com/channel", "", "", false},
		{"https://www.youtube.com/@", "", "", false},
		{"https://example.com/@someone", "", "", false},
	}
	for _, c := range cases {
		home, tab, ok := YoutubeChannel(c.link)
		if home != c.home || tab != c.tab || ok != c.ok {
			t.Errorf("YoutubeChannel(%q) = %q, %q, %v; want %q, %q, %v", c.link, home, tab, ok, c.home, c.tab, c.ok)
		}
	}
}

// Channels start with nothing ticked; playlists with everything. Fields as seen in real probes.
func TestChannelOrPlaylist(t *testing.T) {
	cases := []struct {
		name, json string
		channel    bool
	}{
		{"channel home", `{"_type":"playlist","id":"@RickAstleyYT","channel_id":"UCuAXFkgsw1L7xaCfnd5JJOw","webpage_url":"https://www.youtube.com/@RickAstleyYT","entries":[]}`, true},
		{"channel tab", `{"_type":"playlist","id":"UCuAXFkgsw1L7xaCfnd5JJOw","channel_id":"UCuAXFkgsw1L7xaCfnd5JJOw","webpage_url":"https://www.youtube.com/@RickAstleyYT/videos","entries":[]}`, true},
		{"other site's channel", `{"_type":"playlist","id":"u123","channel_id":"u123","webpage_url":"https://example.com/u/123","entries":[]}`, true},
		{"playlist", `{"_type":"playlist","id":"PLlaN88a7y2_qHDbY9eQbuNTAuEJUSEeuu","channel_id":"UCuAXFkgsw1L7xaCfnd5JJOw","webpage_url":"https://www.youtube.com/playlist?list=PLlaN88a7y2_qHDbY9eQbuNTAuEJUSEeuu","entries":[]}`, false},
		{"no channel given", `{"_type":"playlist","id":"x","webpage_url":"https://example.com/list/x","entries":[]}`, false},
	}
	for _, c := range cases {
		res, err := ParseProbe([]byte(c.json))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Playlist.Channel != c.channel {
			t.Errorf("%s: channel = %v, want %v", c.name, res.Playlist.Channel, c.channel)
		}
	}
}
