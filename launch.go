package main

import (
	"net/url"
	"strings"

	"yt-dlp-gui/internal/linkbridge"
	"yt-dlp-gui/internal/ytdlp"
)

// urlScheme is the link type that opens Prem-dl on a link: prem-dl://open?url=<link>, or with
// the Chrome helper's quick actions prem-dl://download?url=<link>&preset=<name> and
// prem-dl://follow?url=<link>. The helper uses it to start the app when it isn't running (when it
// is, it uses linkbridge). It is registered for the current user at startup.
const urlScheme = "prem-dl"

// backgroundFlag starts the app in the tray, without showing the window. Starting with Windows
// passes it.
const backgroundFlag = "--background"

// launchArgs is what the app was started with, or what a second launch handed over.
type launchArgs struct {
	Link       string // a link to open on the Download page, already checked
	Action     string // what to do with Link: a linkbridge action ("" = open it)
	Preset     string // the preset a download action uses
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
			l.Link, l.Action, l.Preset = schemeLink(a)
		case strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://"):
			if link, err := ytdlp.NormalizeURL(a); err == nil {
				l.Link = link
			}
		}
	}
	return l
}

// schemeLink takes the http(s) link out of a prem-dl: link, with what to do with it. link is ""
// when there's none; an action other than open, download or follow opens it.
func schemeLink(raw string) (link, action, preset string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", ""
	}
	q := u.Query()
	if link, err = ytdlp.NormalizeURL(q.Get("url")); err != nil {
		return "", "", ""
	}
	switch strings.ToLower(u.Host) {
	case linkbridge.ActionDownload:
		return link, linkbridge.ActionDownload, q.Get("preset")
	case linkbridge.ActionFollow:
		return link, linkbridge.ActionFollow, ""
	}
	return link, linkbridge.ActionOpen, ""
}
