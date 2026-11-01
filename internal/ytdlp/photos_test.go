package ytdlp

import (
	"math"
	"strings"
	"testing"
)

// A TikTok photo post (internal/tiktok): a playlist of its sound and pictures, with photo_count.
const photoPost = `{"_type":"playlist","id":"77","title":"Rocket...","extractor_key":"TikTok",
	"webpage_url":"https://www.tiktok.com/@nasa/photo/77","photo_count":2,
	"formats":[{"format_id":"audio","ext":"m4a","vcodec":"none","acodec":"aac","url":"https://s.test/a"}],
	"entries":[{"id":"77","photo_file":"Sound"},{"id":"77-1","photo_file":"01"},{"id":"77-2","photo_file":"02"}]}`

func TestPhotoPostIsOneVideo(t *testing.T) {
	res, err := FromInfo([]byte(photoPost))
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != "video" || res.Video.Photos != 2 || len(res.Video.Formats) != 1 || res.Video.Extractor != "TikTok" {
		t.Fatalf("%+v", res.Video)
	}
	if string(res.info) != photoPost {
		t.Error("the JSON isn't kept for the download")
	}
	// Without photo_count a playlist stays a playlist.
	pl, _ := ParseProbe([]byte(strings.Replace(photoPost, `"photo_count":2,`, "", 1)))
	if pl.Kind != "playlist" {
		t.Errorf("kind %s", pl.Kind)
	}
}

func TestInfoCacheKeepsTikTokPosts(t *testing.T) {
	c := NewInfoCache()
	res, _ := FromInfo([]byte(photoPost))
	c.Put("https://vm.tiktok.com/ZMabc/", res)
	saved, done := c.Take("https://www.tiktok.com/@nasa/photo/77")
	defer done()
	if saved.Path == "" || saved.Photos != 2 {
		t.Fatalf("%+v", saved)
	}
}

func TestPhotoFiles(t *testing.T) {
	o := testEnv().Settings.Defaults
	audio := o
	audio.Preset = "audio"
	custom := o
	custom.Preset, custom.FormatID, custom.FormatAudioOnly = "custom", "audio", true
	if PhotoFiles(o, 0) != 0 || PhotoFiles(o, 5) != 6 {
		t.Error("a photo post is its pictures and its sound; anything else isn't one")
	}
	if PhotoFiles(audio, 5) != 1 || PhotoFiles(custom, 5) != 1 {
		t.Error("an audio preset should take the sound alone")
	}
}

func TestPhotoPostDownloadArgs(t *testing.T) {
	e := testEnv()
	o := e.Settings.Defaults
	o.EmbedThumbnail, o.EmbedMetadata, o.Container = true, true, "mp4"
	args := e.DownloadArgs(o, Target{URL: "https://www.tiktok.com/@nasa/photo/77", InfoFile: "/tmp/i.json", Photos: 5})
	for _, want := range []string{
		"--format=picture/audio", "--no-clean-info-json", "--paths=" + e.Settings.DownloadDir,
		"--output=%(title)s/%(" + PhotoFileField + ")s.%(ext)s",
	} {
		if !has(args, want) {
			t.Errorf("missing %s in %v", want, args)
		}
	}
	for _, a := range args {
		// A video's streams, containers and tags don't apply to pictures.
		if strings.HasPrefix(a, "--embed") || strings.HasPrefix(a, "--merge-output") || strings.HasPrefix(a, "--remux") ||
			strings.HasPrefix(a, "--format-sort") || a == "--no-playlist" || a == "--" {
			t.Errorf("unexpected %s", a)
		}
	}
	if args[len(args)-1] != "--load-info-json=/tmp/i.json" {
		t.Errorf("last argument %s", args[len(args)-1])
	}

	o.Preset, o.AudioFormat = "audio", "mp3"
	args = e.DownloadArgs(o, Target{URL: "https://www.tiktok.com/@nasa/photo/77", InfoFile: "/tmp/i.json", Photos: 5})
	for _, want := range []string{"--extract-audio", "--audio-format=mp3", "--no-clean-info-json", "--playlist-items=1", "--output=%(title)s.%(ext)s"} {
		if !has(args, want) {
			t.Errorf("audio: missing %s in %v", want, args)
		}
	}
}

func TestTrackerPhotoPostIsOneShareEachFile(t *testing.T) {
	// The sound, then two pictures, each reported 0-100% on its own. Pictures count, though
	// their names look like thumbnails.
	tr := Tracker{Files: 3}
	dir := "/dl/Rocket/"
	steps := []struct {
		p     Progress
		want  float64
		done  bool
		stage string
	}{
		{Progress{Status: "downloading", Downloaded: 50, Total: 100, Filename: dir + "Sound.m4a"}, 100.0 / 6, false, "Downloading the sound"},
		{Progress{Status: "finished", Downloaded: 100, Total: 100, Filename: dir + "Sound.m4a"}, 100.0 / 3, false, "Downloading the sound"},
		{Progress{Status: "finished", Downloaded: 40, Total: 40, Filename: dir + "01.jpg"}, 200.0 / 3, false, "Downloading picture 1 of 2"},
		{Progress{Status: "downloading", Downloaded: 10, Total: 40, Filename: dir + "02.jpg"}, 200.0/3 + 100.0/12, false, "Downloading picture 2 of 2"},
		{Progress{Status: "finished", Downloaded: 40, Total: 40, Filename: dir + "02.jpg"}, 100, true, "Downloading picture 2 of 2"},
	}
	for i, s := range steps {
		snap, ok := tr.Update(s.p)
		if !ok || math.Abs(snap.Percent-s.want) > 0.1 || tr.AllDone() != s.done || snap.Stage != s.stage {
			t.Fatalf("step %d: ok=%v %.1f%% done=%v %q, want %.1f%% done=%v %q", i, ok, snap.Percent, tr.AllDone(), snap.Stage, s.want, s.done, s.stage)
		}
	}
}
