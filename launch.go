package main

import (
	"net/netip"
	"net/url"
	"strings"

	"yt-dlp-gui/internal/linkbridge"
	"yt-dlp-gui/internal/ytdlp"
)

// urlScheme is the link type that opens Prem-dl on a link: prem-dl://open?url=<link>, or
// prem-dl://follow?url=<link> for the follow dialog. The helper uses it to start the app when it isn't running (when it
// is, it uses linkbridge). It is registered for the current user at startup.
const urlScheme = "prem-dl"

// backgroundFlag starts the app in the tray, without showing the window. Starting with Windows
// passes it.
const backgroundFlag = "--background"

// launchArgs is what the app was started with, or what a second launch handed over.
type launchArgs struct {
	Link       string // a link to open on the Download page, already checked
	Action     string // what to do with Link: open it ("") or follow it
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
			l.Link, l.Action = schemeLink(a)
		case strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://"):
			if link, err := ytdlp.NormalizeURL(a); err == nil && !localHost(link) {
				l.Link = link
			}
		}
	}
	return l
}

// schemeLink takes the http(s) link out of a prem-dl: link, with what to do with it. link is ""
// when there's none.
//
// Any web page can open a prem-dl: link (once the browser has asked), so what one does has to be
// harmless without the user's say-so: it opens the link for review, or the follow dialog. A
// download (prem-dl://download, the helper's Download now while Prem-dl isn't running) goes to
// the Download page too, and links to this computer or the local network are refused, so a page
// can't make Prem-dl fetch them. The helper reaches a running Prem-dl through linkbridge, which only it can
// use, and only there does Download now queue at once.
func schemeLink(raw string) (link, action string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	if link, err = ytdlp.NormalizeURL(u.Query().Get("url")); err != nil || localHost(link) {
		return "", ""
	}
	if strings.EqualFold(u.Host, linkbridge.ActionFollow) {
		return link, linkbridge.ActionFollow
	}
	return link, linkbridge.ActionOpen
}

// localHost reports whether link points at this computer or the local network: localhost, or
// an address in a loopback, private or link-local range.
func localHost(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return true
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false // a name: what it resolves to is the site's business
	}
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast()
}
