package follow

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"

	"yt-dlp-gui/internal/settings"
)

// Moving follows between computers, and starting from YouTube's own list of subscriptions.

// exportFormat marks a file written by Export.
const exportFormat = "prem-dl follows"

type exported struct {
	Format  string       `json:"format"`
	Version int          `json:"version"`
	Feeds   []exportFeed `json:"feeds"`
}

type exportFeed struct {
	URL      string                   `json:"url"`
	Title    string                   `json:"title"`
	Uploader string                   `json:"uploader"`
	Tabs     []string                 `json:"tabs"`
	Options  settings.DownloadOptions `json:"options"`
	Filter   Filter                   `json:"filter"`
	// RecordLive: see Feed.RecordLive.
	RecordLive bool `json:"recordLive"`
	Paused     bool `json:"paused"`
}

// Export writes feeds as a file Import reads back: links, tabs, options and filters. What each
// feed has seen stays behind; following again on another computer notes what's there then.
func Export(feeds []Feed) ([]byte, error) {
	out := exported{Format: exportFormat, Version: 1, Feeds: []exportFeed{}}
	for _, f := range feeds {
		out.Feeds = append(out.Feeds, exportFeed{URL: f.URL, Title: f.Title, Uploader: f.Uploader, Tabs: f.Tabs, Options: f.Options, Filter: f.Filter, RecordLive: f.RecordLive, Paused: f.Paused})
	}
	return json.MarshalIndent(out, "", "  ")
}

// Imported is a feed read from a file, to follow.
type Imported struct {
	NewFeed
	Paused bool
	// HasOptions is set when the file says how to download (a Prem-dl export); otherwise the
	// default options apply.
	HasOptions bool
}

// MaxImport bounds how many feeds one file may add.
const MaxImport = 2000

var channelIDRe = regexp.MustCompile(`^UC[0-9A-Za-z_-]{22}$`)

// ParseImport reads a Prem-dl export, Google Takeout's YouTube subscriptions.csv (Channel Id,
// Channel Url, Channel Title) or an OPML list of YouTube channel feeds.
func ParseImport(data []byte) ([]Imported, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // Takeout writes a BOM
	trimmed := bytes.TrimSpace(data)
	var out []Imported
	switch {
	case bytes.HasPrefix(trimmed, []byte("{")):
		var e exported
		if err := json.Unmarshal(trimmed, &e); err != nil || e.Format != exportFormat {
			return nil, errors.New("this isn't a file of follows exported from Prem-dl")
		}
		for _, f := range e.Feeds {
			out = append(out, Imported{
				NewFeed: NewFeed{URL: f.URL, Title: f.Title, Uploader: f.Uploader, Tabs: f.Tabs, Options: f.Options, Filter: f.Filter, RecordLive: f.RecordLive},
				Paused:  f.Paused, HasOptions: true,
			})
		}
	case bytes.HasPrefix(trimmed, []byte("<")):
		feeds, err := parseOPML(trimmed)
		if err != nil {
			return nil, err
		}
		out = feeds
	default:
		feeds, err := parseTakeout(trimmed)
		if err != nil {
			return nil, err
		}
		out = feeds
	}
	if len(out) == 0 {
		return nil, errors.New("the file lists no channels or playlists")
	}
	if len(out) > MaxImport {
		return nil, errors.New("the file lists more than 2,000 channels; split it up")
	}
	return out, nil
}

func channel(id, title string) Imported {
	return Imported{NewFeed: NewFeed{URL: "https://www.youtube.com/channel/" + id, Title: title, Uploader: title, Tabs: []string{"videos"}}}
}

func parseTakeout(data []byte) ([]Imported, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return nil, errors.New("this file isn't a list of follows Prem-dl knows: use a Prem-dl export, YouTube's subscriptions.csv from Google Takeout, or an OPML file")
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	idCol, ok := col["channel id"]
	if !ok {
		return nil, errors.New("this CSV has no Channel Id column. Use the subscriptions.csv from Google Takeout")
	}
	titleCol, hasTitle := col["channel title"]
	var out []Imported
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("the CSV couldn't be read: " + err.Error())
		}
		if idCol >= len(rec) {
			continue
		}
		id := strings.TrimSpace(rec[idCol])
		if !channelIDRe.MatchString(id) {
			continue
		}
		title := ""
		if hasTitle && titleCol < len(rec) {
			title = strings.TrimSpace(rec[titleCol])
		}
		out = append(out, channel(id, title))
	}
	return out, nil
}

type opmlOutline struct {
	Title    string        `xml:"title,attr"`
	Text     string        `xml:"text,attr"`
	XMLURL   string        `xml:"xmlUrl,attr"`
	Outlines []opmlOutline `xml:"outline"`
}

// parseOPML takes the YouTube channel feeds of an OPML file
// (https://www.youtube.com/feeds/videos.xml?channel_id=UC...), as feed readers export them.
func parseOPML(data []byte) ([]Imported, error) {
	var doc struct {
		Outlines []opmlOutline `xml:"body>outline"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, errors.New("the OPML file couldn't be read: " + err.Error())
	}
	var out []Imported
	var walk func([]opmlOutline, int)
	walk = func(items []opmlOutline, depth int) {
		for _, o := range items {
			if u, err := url.Parse(o.XMLURL); err == nil && strings.HasSuffix(u.Hostname(), "youtube.com") {
				if id := u.Query().Get("channel_id"); channelIDRe.MatchString(id) {
					title := o.Title
					if title == "" {
						title = o.Text
					}
					out = append(out, channel(id, strings.TrimSpace(title)))
				}
			}
			if depth < 3 {
				walk(o.Outlines, depth+1)
			}
		}
	}
	walk(doc.Outlines, 0)
	return out, nil
}
