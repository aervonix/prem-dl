package follow

import (
	"testing"
)

func TestParseTakeout(t *testing.T) {
	csv := "\xef\xbb\xbfChannel Id,Channel Url,Channel Title\n" +
		"UCsXVk37bltHxD1rDPwtNM8Q,http://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q,Kurzgesagt – In a Nutshell\n" +
		"not-an-id,http://x,Bad\n" +
		"UCHnyfMqiRRG1u-2MsSQLbXA,http://www.youtube.com/channel/UCHnyfMqiRRG1u-2MsSQLbXA,\"Veritasium, the channel\"\n"
	got, err := ParseImport([]byte(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].URL != "https://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q" || got[1].Title != "Veritasium, the channel" || got[0].HasOptions {
		t.Fatalf("%+v", got)
	}
	if len(got[0].Tabs) != 1 || got[0].Tabs[0] != "videos" {
		t.Errorf("tabs %v", got[0].Tabs)
	}
}

func TestParseOPML(t *testing.T) {
	opml := `<?xml version="1.0"?><opml version="1.1"><body><outline text="YouTube Subscriptions" title="YouTube Subscriptions">
	<outline text="Kurzgesagt" title="Kurzgesagt" type="rss" xmlUrl="https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q"/>
	<outline text="A blog" type="rss" xmlUrl="https://example.com/feed"/></outline></body></opml>`
	got, err := ParseImport([]byte(opml))
	if err != nil || len(got) != 1 || got[0].Title != "Kurzgesagt" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	o := opts()
	o.Preset = "720"
	feeds := []Feed{{URL: "https://www.youtube.com/@chan", Title: "Chan", Tabs: []string{"videos", "shorts"}, Options: o, Filter: Filter{MaxMinutes: 90, Include: []string{}, Exclude: []string{"#shorts"}}, Paused: true}}
	data, err := Export(feeds)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseImport(data)
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	g := got[0]
	if !g.HasOptions || !g.Paused || g.Options.Preset != "720" || g.Filter.MaxMinutes != 90 || len(g.Tabs) != 2 {
		t.Errorf("%+v", g)
	}
}

func TestParseImportRefusesOtherFiles(t *testing.T) {
	for _, bad := range []string{"", "hello", `{"format":"something else"}`, "a,b\n1,2\n", "<opml><body></body></opml>"} {
		if _, err := ParseImport([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
