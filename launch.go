package main

import (
	"net/netip"
	"net/url"
	"strconv"
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
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		var ok bool
		if ip, ok = inetAton(host); !ok {
			return false // a name: what it resolves to is the site's business
		}
	}
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast()
}

// inetAton reads the IPv4 forms the system's resolver also takes for an address, though
// ParseAddr doesn't: fewer than four parts ("127.1"), one number ("2130706433"), hex ("0x7f.1")
// and octal ("0177.0.0.1"). A host made only of such parts is one of these.
func inetAton(host string) (netip.Addr, bool) {
	parts := strings.Split(host, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return netip.Addr{}, false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 0, 32) // 0x.. hex, 0.. octal, else decimal
		if p == "" || err != nil {
			return netip.Addr{}, false
		}
		nums[i] = n
	}
	// The last part fills the bytes the others don't: a.b.c.d, a.b.(16 bits), a.(24 bits), (32 bits).
	var v uint64
	for i, n := range nums[:len(nums)-1] {
		if n > 255 {
			return netip.Addr{}, false
		}
		v |= n << (24 - 8*i)
	}
	last := nums[len(nums)-1]
	if bits := 32 - 8*(len(nums)-1); last >= 1<<bits {
		return netip.Addr{}, false
	}
	v |= last
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true
}
