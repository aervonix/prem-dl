package ytweb

import (
	"encoding/json"
	"testing"
	"time"
)

// A scheduled stream gives its start as yt-dlp does (release_timestamp), so a followed channel
// can tell one due tomorrow from a placeholder months away.
func TestUpcomingStreamHasItsStart(t *testing.T) {
	var r map[string]any
	_ = json.Unmarshal([]byte(`{"videoId":"abcdefghijk","title":{"runs":[{"text":"Stream in 2027"}]},
	  "upcomingEventData":{"startTime":"1830000000","isReminderSet":false},
	  "navigationEndpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/watch?v=abcdefghijk"}}}}`), &r)
	e := extractor{now: time.Now(), dates: true}.video(r)
	if e.LiveStatus != "is_upcoming" || e.ReleaseTimestamp != 1830000000 {
		t.Fatalf("%+v", e)
	}
}
