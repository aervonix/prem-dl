package main

import (
	"net/url"
	"strings"

	"yt-dlp-gui/internal/ytdlp"
)

// urlScheme is the link type that opens Prem-dl on a link: prem-dl://open?url=<link>. The Chrome
// helper uses it to start the app when it isn't running (when it is, it uses linkbridge). It is
// registered for the current user at startup.
const urlScheme = "prem-dl"

// backgroundFlag starts the app in the tray, without showing the window. Starting with Windows
// passes it.
const backgroundFlag = "--background"

// launchArgs is what the app was started with, or what a second launch handed over.
type launchArgs struct {
	Link       string // a link to open on the Download page, already checked
	Background bool   // stay in the tray
}

// parseLaunch reads the command line. Anything it doesn't recognise is ignored: Windows adds its
// own arguments when a notification starts the app.
func parseLaunch(args []string) launchArgs {
	var l launchArgs
	for _, a := range args {
		a = strings.TrimSpace(a)
		switch {
		case a == backgroundFlag:
			l.Background = true
		case l.Link != "":
		case strings.HasPrefix(strings.ToLower(a), urlScheme+":"):
			l.Link = schemeLink(a)
		case strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://"):
			if link, err := ytdlp.NormalizeURL(a); err == nil {
				l.Link = link
			}
		}
	}
	return l
}

// schemeLink takes the http(s) link out of a prem-dl: link, or returns "".
func schemeLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	link, err := ytdlp.NormalizeURL(u.Query().Get("url"))
	if err != nil {
		return ""
	}
	return link
}
