package ytdlp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LineKind says what a line of yt-dlp output turned out to be.
type LineKind int

const (
	KindOther LineKind = iota
	KindProgress
	KindPlan
	KindStreams
	KindPost
	KindFile
	// KindWait starts a --wait-for-video pause for a scheduled stream ("[wait] Waiting for
	// 77:19:39 - Press Ctrl+C to try now"); KindWaitTick is its countdown, printed every second.
	KindWait
	KindWaitTick
)

var waitRe = regexp.MustCompile(`^\[wait\] Waiting for (\d+):(\d\d):(\d\d)`)

// Progress is one `%(progress)j` report.
type Progress struct {
	Status        string
	Downloaded    int64
	Total         int64
	TotalEstimate int64
	Speed         float64
	ETA           int // seconds; -1 when unknown
	Filename      string
	Fragment      int
	Fragments     int
}

// Plan is the metadata printed before a download starts.
type Plan struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Uploader  string  `json:"uploader"`
	Thumbnail string  `json:"thumbnail"`
	Duration  float64 `json:"duration"`
	Filesize  float64 `json:"filesize"`
	Approx    float64 `json:"filesize_approx"`
}

// Size is the best size estimate for the whole download, 0 if unknown.
func (p Plan) Size() int64 {
	if p.Filesize > 0 {
		return int64(p.Filesize)
	}
	return int64(p.Approx)
}

// Stream is one of the files that will be downloaded and merged.
type Stream struct {
	Size int64
	Kind string // video | audio | ""
}

// Line is a parsed line of output. Only the field matching Kind is meaningful.
type Line struct {
	Kind     LineKind
	Progress Progress
	Plan     Plan
	Streams  []Stream
	PostName string
	PostStat string
	File     string
	Wait     time.Duration // until yt-dlp looks at a scheduled stream again
}

// ParseLine classifies one line of yt-dlp output. Unrecognised lines come back as KindOther.
func ParseLine(line string) Line {
	line = strings.TrimRight(line, "\r\n")
	switch {
	case strings.HasPrefix(line, MarkProgress):
		if p, ok := parseProgress(line[len(MarkProgress):]); ok {
			return Line{Kind: KindProgress, Progress: p}
		}
	case strings.HasPrefix(line, MarkPlan):
		var p Plan
		if json.Unmarshal([]byte(line[len(MarkPlan):]), &p) == nil {
			return Line{Kind: KindPlan, Plan: p}
		}
	case strings.HasPrefix(line, MarkStreams):
		return Line{Kind: KindStreams, Streams: parseStreams(line[len(MarkStreams):])}
	case strings.HasPrefix(line, MarkPost):
		name, status, _ := strings.Cut(line[len(MarkPost):], "|")
		return Line{Kind: KindPost, PostName: name, PostStat: strings.TrimSpace(status)}
	case strings.HasPrefix(line, MarkFile):
		if f := strings.TrimSpace(line[len(MarkFile):]); f != "" && f != "NA" {
			return Line{Kind: KindFile, File: f}
		}
	case strings.HasPrefix(line, "[wait] Remaining time"):
		return Line{Kind: KindWaitTick}
	case strings.HasPrefix(line, "[wait] Waiting for"):
		if m := waitRe.FindStringSubmatch(line); m != nil {
			h, _ := strconv.Atoi(m[1])
			mi, _ := strconv.Atoi(m[2])
			s, _ := strconv.Atoi(m[3])
			return Line{Kind: KindWait, Wait: time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute + time.Duration(s)*time.Second}
		}
	}
	return Line{Kind: KindOther}
}

func parseProgress(js string) (Progress, bool) {
	var raw struct {
		Status        string   `json:"status"`
		Downloaded    float64  `json:"downloaded_bytes"`
		Total         float64  `json:"total_bytes"`
		TotalEstimate float64  `json:"total_bytes_estimate"`
		Speed         *float64 `json:"speed"`
		ETA           *float64 `json:"eta"`
		Filename      string   `json:"filename"`
		Fragment      float64  `json:"fragment_index"`
		Fragments     float64  `json:"fragment_count"`
	}
	if json.Unmarshal([]byte(js), &raw) != nil {
		return Progress{}, false
	}
	p := Progress{
		Status: raw.Status, Downloaded: int64(raw.Downloaded), Total: int64(raw.Total),
		TotalEstimate: int64(raw.TotalEstimate), ETA: -1, Filename: raw.Filename,
		Fragment: int(raw.Fragment), Fragments: int(raw.Fragments),
	}
	if raw.Speed != nil {
		p.Speed = *raw.Speed
	}
	if raw.ETA != nil {
		p.ETA = int(*raw.ETA)
	}
	return p, true
}

func parseStreams(js string) []Stream {
	var raw []struct {
		Filesize float64 `json:"filesize"`
		Approx   float64 `json:"filesize_approx"`
		VCodec   string  `json:"vcodec"`
		ACodec   string  `json:"acodec"`
	}
	if json.Unmarshal([]byte(js), &raw) != nil { // "NA" or null when nothing is being merged
		return nil
	}
	out := make([]Stream, 0, len(raw))
	for _, r := range raw {
		s := Stream{Size: int64(r.Filesize)}
		if s.Size == 0 {
			s.Size = int64(r.Approx)
		}
		switch {
		case r.VCodec != "" && r.VCodec != "none":
			s.Kind = "video"
		case r.ACodec != "" && r.ACodec != "none":
			s.Kind = "audio"
		}
		out = append(out, s)
	}
	return out
}

// sideFileExts are files yt-dlp downloads alongside the media (subtitles, thumbnails, ...).
// Their progress reports must not be mistaken for a media stream.
var sideFileExts = map[string]bool{
	".vtt": true, ".srt": true, ".ass": true, ".ttml": true, ".srv1": true, ".srv2": true, ".srv3": true,
	".json3": true, ".lrc": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".description": true, ".json": true,
}

func isSideFile(name string) bool {
	return sideFileExts[strings.ToLower(filepath.Ext(strings.TrimSuffix(name, ".part")))]
}

// PostStage is a human description of what a post-processor is doing.
func PostStage(name string) string {
	switch name {
	case "Merger":
		return "Merging video and audio"
	case "EmbedSubtitle":
		return "Embedding subtitles"
	case "Metadata", "FFmpegMetadata":
		return "Writing metadata"
	case "EmbedThumbnail":
		return "Embedding thumbnail"
	case "ExtractAudio", "FFmpegExtractAudio":
		return "Converting audio"
	case "SubtitlesConvertor", "FFmpegSubtitlesConvertor":
		return "Converting subtitles"
	case "ThumbnailsConvertor", "FFmpegThumbnailsConvertor":
		return "Converting thumbnail"
	case "ModifyChapters":
		return "Cutting sponsor segments"
	case "SponsorBlock":
		return "Looking up SponsorBlock segments"
	case "VideoRemuxer", "FFmpegVideoRemuxer":
		return "Remuxing"
	case "VideoConvertor":
		return "Converting video"
	case "MoveFiles":
		return "Finishing up"
	case "FixupM3u8", "FixupM4a", "FixupStretched", "FixupTimestamp", "FixupDuration":
		return "Repairing container"
	default:
		return "Processing"
	}
}

// Snapshot is the aggregate state of a download for display.
type Snapshot struct {
	Percent    float64 // 0-100; -1 when no meaningful figure exists yet
	Downloaded int64
	Total      int64
	Speed      float64
	ETA        int
	Stage      string
}

// Tracker folds progress reports from multiple streams into one overall figure.
// yt-dlp reports each stream (video, then audio) as 0-100% on its own.
type Tracker struct {
	// Cut is set for a time-range download. The plan's sizes are for the whole video, and yt-dlp
	// fetches every stream in one ffmpeg run into the final file, reporting only that file.
	Cut bool
	// Sections is how many cuts one run downloads (chosen chapters), one file after another,
	// each reported 0-100% on its own; 0 or 1 for a single cut.
	Sections int

	streams   []Stream
	planTotal int64

	idx       int
	lastFile  string
	lastBytes int64 // bytes of the in-flight stream at its last report
	doneBytes int64 // bytes of streams that finished
	sawStream bool
	allDone   bool
}

// AllDone reports whether every media stream has finished downloading. Post-processors can run
// before that (subtitle conversion happens before the video is fetched), so callers must not
// treat "a post-processor started" as "the download is complete".
func (t *Tracker) AllDone() bool { return t.allDone }

// SetStreams records the streams that will be merged.
func (t *Tracker) SetStreams(s []Stream) { t.streams = s }

// SetPlan records the whole-download size estimate.
func (t *Tracker) SetPlan(p Plan) { t.planTotal = p.Size() }

// formatFileRe matches the per-stream files of a merged download ("title [id].f137.mp4").
var formatFileRe = regexp.MustCompile(`\.f[^.\\/]+\.[^.\\/]+$`)

func (t *Tracker) expectedTotal(cur Progress) int64 {
	if t.Cut {
		return max(cur.Total, cur.TotalEstimate)
	}
	if len(t.streams) > 0 {
		var sum int64
		for _, s := range t.streams {
			if s.Size == 0 {
				sum = 0
				break
			}
			sum += s.Size
		}
		if sum > 0 {
			return sum
		}
	} else if t.planTotal > 0 {
		return t.planTotal
	}
	// Sizes unknown up front: the best we can say is what's finished plus the current stream.
	cs := cur.Total
	if cs == 0 {
		cs = cur.TotalEstimate
	}
	if cs == 0 {
		return 0
	}
	return t.doneBytes + cs
}

func (t *Tracker) stageLabel() string {
	if len(t.streams) > 1 && t.idx < len(t.streams) {
		switch t.streams[t.idx].Kind {
		case "video":
			return "Downloading video"
		case "audio":
			return "Downloading audio"
		}
		return "Downloading"
	}
	return "Downloading"
}

// Update ingests a progress report. ok is false for reports that should be ignored.
func (t *Tracker) Update(p Progress) (Snapshot, bool) {
	if isSideFile(p.Filename) {
		return Snapshot{}, false
	}
	if t.sawStream && p.Filename != t.lastFile {
		// A new stream began: bank the previous one.
		t.doneBytes += t.lastBytes
		t.idx++
		t.lastBytes = 0
	}
	if p.Status == "finished" && p.Downloaded == 0 {
		// A stream that was already on disk is reported finished with only its size.
		p.Downloaded = max(p.Total, p.TotalEstimate)
	}
	t.sawStream = true
	t.lastFile = p.Filename
	t.lastBytes = p.Downloaded
	// A file that isn't one of the per-stream parts means the streams were fetched together.
	merged := !formatFileRe.MatchString(strings.TrimSuffix(p.Filename, ".part"))
	t.allDone = p.Status == "finished" && (len(t.streams) <= 1 || t.idx >= len(t.streams)-1 || merged)
	if t.Cut && t.Sections > 1 {
		t.allDone = p.Status == "finished" && t.idx >= t.Sections-1
	}

	snap := Snapshot{
		Downloaded: t.doneBytes + p.Downloaded,
		Speed:      p.Speed,
		ETA:        p.ETA,
		Percent:    -1,
		Stage:      t.stageLabel(),
	}
	if t.Cut || merged {
		snap.Stage = "Downloading"
	}
	total := t.expectedTotal(p)
	if total > 0 {
		snap.Total = total
		snap.Percent = float64(snap.Downloaded) / float64(total) * 100
	}
	if t.Cut && t.Sections > 1 {
		// Only the section in flight has a size: count each as an equal share.
		snap.Total = t.doneBytes + max(p.Total, p.TotalEstimate)
		snap.Percent = float64(min(t.idx, t.Sections-1)) / float64(t.Sections) * 100
		if cs := max(p.Total, p.TotalEstimate); cs > 0 {
			snap.Percent += min(float64(p.Downloaded)/float64(cs), 1) / float64(t.Sections) * 100
		}
		snap.Stage = fmt.Sprintf("Downloading chapter %d of %d", min(t.idx, t.Sections-1)+1, t.Sections)
	}
	if n := len(t.streams); n > 1 && !t.Cut && !merged && !t.sizesKnown() {
		// Only the stream in flight has a size: count each stream as an equal share, so the bar
		// doesn't reach 100% at the end of the video and fall back when the audio starts.
		if cs := max(p.Total, p.TotalEstimate); cs > 0 {
			frac := min(float64(p.Downloaded)/float64(cs), 1)
			snap.Percent = (float64(min(t.idx, n-1)) + frac) / float64(n) * 100
		}
	}
	if !t.allDone && snap.Percent > 99.9 {
		// Size estimates run low; don't show a full bar while something is still downloading.
		snap.Percent = 99.9
	}
	snap.Percent = min(snap.Percent, 100)
	if len(t.streams) > 1 && snap.Total > 0 && p.Speed > 0 && !t.Cut {
		// The per-stream ETA only covers the stream in flight; recompute from the overall remainder.
		snap.ETA = max(int(float64(snap.Total-snap.Downloaded)/p.Speed), 0)
	}
	return snap, true
}

func (t *Tracker) sizesKnown() bool {
	for _, s := range t.streams {
		if s.Size == 0 {
			return false
		}
	}
	return len(t.streams) > 0
}
