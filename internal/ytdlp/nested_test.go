package ytdlp

import "testing"

func TestEmptyNestedTabIsNoEntry(t *testing.T) {
	res, err := ParseProbe([]byte(`{"_type":"playlist","title":"Chan","webpage_url":"https://www.youtube.com/@x","entries":[
	 {"_type":"playlist","title":"Chan - Videos","entries":[]},
	 {"_type":"playlist","title":"Chan - Shorts","entries":[{"_type":"url","id":"a","url":"https://www.youtube.com/shorts/a","title":"A"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Playlist.Entries {
		t.Logf("%+v", e)
	}
	if len(res.Playlist.Entries) != 1 {
		t.Errorf("an empty tab became an entry: %d entries", len(res.Playlist.Entries))
	}
}
