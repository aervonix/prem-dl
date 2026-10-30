package ytdlp

import (
	"context"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"yt-dlp-gui/internal/apperr"
)

// SearchFilter narrows a YouTube search the way the filter menu on its results page does.
// YouTube no longer honours its sort orders (by upload date, by views), so there is none here.
type SearchFilter struct {
	Type     string `json:"type"`     // "" for everything, video, channel or playlist
	Uploaded string `json:"uploaded"` // "" for any time, hour, today, week, month or year
	Length   string `json:"length"`   // "" for any, short (under 4 min), medium (4-20 min) or long (over 20)
}

// The values of the search filter's protobuf fields: 1 upload date, 2 type, 3 duration.
var (
	searchUploaded = map[string]byte{"hour": 1, "today": 2, "week": 3, "month": 4, "year": 5}
	searchType     = map[string]byte{"video": 1, "channel": 2, "playlist": 3}
	searchLength   = map[string]byte{"short": 1, "long": 2, "medium": 3}
)

// MaxQuery is the longest search accepted, in characters; YouTube's own box stops at about 100.
const MaxQuery = 200

// SearchURL is YouTube's results page for query, with the filter as its "sp" parameter.
func SearchURL(query string, f SearchFilter) string {
	q := url.Values{"search_query": {strings.TrimSpace(query)}}
	if sp := SearchParams(f); sp != "" {
		q.Set("sp", sp)
	}
	return "https://www.youtube.com/results?" + q.Encode()
}

// SearchParams encodes the filter the way YouTube's results page does: a protobuf message
// {2: {1: uploaded, 2: type, 3: length}}, base64-encoded; "" for no filter. Observed values
// match YouTube's own: videos only is EgIQAQ==, this week's videos EgQIAxAB.
func SearchParams(f SearchFilter) string {
	var inner []byte
	for i, v := range []byte{searchUploaded[f.Uploaded], searchType[f.Type], searchLength[f.Length]} {
		if v != 0 {
			inner = append(inner, byte(i+1)<<3, v) // field i+1, varint
		}
	}
	if len(inner) == 0 {
		return ""
	}
	msg := append([]byte{2<<3 | 2, byte(len(inner))}, inner...) // field 2, length-delimited
	return base64.StdEncoding.EncodeToString(msg)
}

// CleanQuery trims a search and refuses an empty or overlong one.
func CleanQuery(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", apperr.New("empty_search", "Nothing to search for", "Type what you're looking for.", "", apperr.ActionNone)
	}
	if utf8.RuneCountInString(query) > MaxQuery {
		return "", apperr.New("long_search", "That search is too long",
			"Searches can be up to "+strconv.Itoa(MaxQuery)+" characters.", "Shorten it and try again.", apperr.ActionNone)
	}
	return query, nil
}

// Search lists YouTube's results for query, count of them starting at the 1-based position
// first. Entries are numbered from first. Channels and playlists among them are IsPlaylist.
func Search(ctx context.Context, env Env, query string, f SearchFilter, first, count int) (*ProbeResult, error) {
	query, err := CleanQuery(query)
	if err != nil {
		return nil, err
	}
	first, count = max(first, 1), max(count, 1)
	args := []string{"--playlist-items=" + strconv.Itoa(first) + ":" + strconv.Itoa(first+count-1)}
	res, err := readInfo(ctx, env, SearchURL(query, f), args)
	if err != nil {
		return nil, err
	}
	if res.Kind != "playlist" {
		// One result comes back as the video itself; nothing comes back as an empty listing.
		return nil, apperr.New("search_failed", "Search didn't work", "YouTube's answer wasn't a list of results.", "Try again in a moment.", apperr.ActionNone)
	}
	for i := range res.Playlist.Entries {
		res.Playlist.Entries[i].Index = first + i
	}
	return res, nil
}
