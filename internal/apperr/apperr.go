// Package apperr turns raw yt-dlp output into errors a person can act on.
package apperr

import (
	"regexp"
	"strings"
)

// Actions tell the UI which button to offer next to an error.
const (
	ActionNone        = ""
	ActionUpdate      = "update"
	ActionCookies     = "cookies"
	ActionInstallBin  = "install_ytdlp"
	ActionSettings    = "settings"
	ActionInstallFFmp = "install_ffmpeg"
)

// AppError is a structured, user-facing error. It is sent to the frontend as JSON.
type AppError struct {
	Code      string `json:"code"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Hint      string `json:"hint"`
	Action    string `json:"action"`
	Detail    string `json:"detail"`
	Retryable bool   `json:"retryable"`
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return e.Title + ": " + e.Message
}

// New builds an AppError for failures that do not come from yt-dlp output.
func New(code, title, message, hint, action string) *AppError {
	return &AppError{Code: code, Title: title, Message: message, Hint: hint, Action: action}
}

type rule struct {
	re    *regexp.Regexp
	build func() AppError
}

func r(pattern string, build func() AppError) rule {
	return rule{re: regexp.MustCompile(`(?i)` + pattern), build: build}
}

// Order matters: more specific rules must come before broader ones.
var rules = []rule{
	r(`failed to decrypt with dpapi`, func() AppError {
		return AppError{
			Code:    "cookie_decrypt",
			Title:   "Can't read browser cookies",
			Message: "Chrome, Edge and other Chromium browsers on Windows now encrypt their cookies in a way yt-dlp cannot read (yt-dlp issue #10927).",
			Hint:    "Use Firefox as the cookie source, or export a cookies.txt file from your browser and import it under Settings → Cookies.",
			Action:  ActionCookies,
		}
	}),
	r(`could not find \w+ cookies? database|cookies? database.*not found|no such (browser|profile)`, func() AppError {
		return AppError{
			Code:    "cookie_browser_missing",
			Title:   "Browser cookies not found",
			Message: "The selected browser (or profile) has no cookie database on this computer.",
			Hint:    "Pick an installed browser and the right profile name, or import a cookies.txt file instead.",
			Action:  ActionCookies,
		}
	}),
	r(`could not copy .*cookie|database is locked|cookies?.*(permission denied|being used by another process)`, func() AppError {
		return AppError{
			Code:      "cookie_locked",
			Title:     "Browser cookie database is locked",
			Message:   "The browser is holding its cookie database open, so yt-dlp can't copy it.",
			Hint:      "Fully close the browser (check the system tray) and try again, or import a cookies.txt file.",
			Action:    ActionCookies,
			Retryable: true,
		}
	}),
	r(`sign in to confirm (you.?re|you are) not a bot|confirm you.?re not a bot`, func() AppError {
		return AppError{
			Code:    "bot_check",
			Title:   "YouTube is asking for sign-in",
			Message: "YouTube flagged this connection as automated and wants a signed-in session.",
			Hint:    "Provide cookies from a browser where you're signed in to YouTube (Settings → Cookies), then retry.",
			Action:  ActionCookies,
		}
	}),
	r(`confirm your age|age.?restrict|inappropriate for some users`, func() AppError {
		return AppError{
			Code:    "age_restricted",
			Title:   "Age-restricted video",
			Message: "This video requires a signed-in, age-verified account.",
			Hint:    "Provide cookies from a signed-in browser session (Settings → Cookies), then retry.",
			Action:  ActionCookies,
		}
	}),
	r(`private video|this video is private|video is private`, func() AppError {
		return AppError{
			Code:    "private",
			Title:   "Private video",
			Message: "Only the owner (or approved viewers) can watch this video.",
			Hint:    "If your account has access, provide cookies from that signed-in session (Settings → Cookies).",
			Action:  ActionCookies,
		}
	}),
	r(`members.?only|join this channel|available to this channel's members`, func() AppError {
		return AppError{
			Code:    "members_only",
			Title:   "Members-only video",
			Message: "This video is limited to channel members.",
			Hint:    "If you are a member, provide cookies from your signed-in session (Settings → Cookies).",
			Action:  ActionCookies,
		}
	}),
	r(`login required|requires? (a )?(login|authentication)|authentication required|use --cookies|sign in|log in to`, func() AppError {
		return AppError{
			Code:    "auth_required",
			Title:   "Sign-in required",
			Message: "This site wants you to be logged in to view this content.",
			Hint:    "Provide cookies from a browser where you're logged in (Settings → Cookies), then retry.",
			Action:  ActionCookies,
		}
	}),
	r(`not available in your country|geo.?restrict|blocked it in your country|not made this video available in your country`, func() AppError {
		return AppError{
			Code:    "geo_blocked",
			Title:   "Not available in your region",
			Message: "The uploader has blocked this content in your country.",
			Hint:    "Route the download through a proxy in an allowed country (Settings → Network → Proxy).",
			Action:  ActionSettings,
		}
	}),
	// Network problems are checked before the generic "unable to download webpage" rule.
	r(`getaddrinfo failed|name or service not known|temporary failure in name resolution|network is unreachable|timed out|timeout|connection (reset|refused|aborted)|remote end closed|winerror 100(51|52|53|54|60|61)|ssl:|proxy error|tunnel connection failed|transporterror`, func() AppError {
		return AppError{
			Code:      "network",
			Title:     "Network problem",
			Message:   "yt-dlp couldn't reach the server (offline, DNS failure, timeout or blocked connection).",
			Hint:      "Check your internet connection, VPN or proxy settings, then retry.",
			Action:    ActionSettings,
			Retryable: true,
		}
	}),
	r(`http error 429|too many requests|rate.?limit`, func() AppError {
		return AppError{
			Code:      "rate_limited",
			Title:     "Rate limited by the site",
			Message:   "The site is throttling requests from this connection (HTTP 429).",
			Hint:      "Wait a few minutes, lower the number of simultaneous downloads, or use signed-in cookies.",
			Action:    ActionSettings,
			Retryable: true,
		}
	}),
	r(`unsupported url`, func() AppError {
		return AppError{
			Code:    "unsupported_url",
			Title:   "Unsupported link",
			Message: "yt-dlp doesn't recognise this URL as a video or playlist page.",
			Hint:    "Make sure you copied the link to the video page itself. If the site is supported, updating yt-dlp may add support.",
			Action:  ActionUpdate,
		}
	}),
	r(`is not a valid url|invalid url|at least one url`, func() AppError {
		return AppError{
			Code:    "invalid_url",
			Title:   "That doesn't look like a link",
			Message: "The text you entered isn't a valid URL.",
			Hint:    "Paste a full link starting with http:// or https://.",
		}
	}),
	r(`ffmpeg.*(not found|not installed|is not available)|ffprobe.*not found|requires ffmpeg|ffmpeg or avconv|postprocessing:.*ffmpeg`, func() AppError {
		return AppError{
			Code:    "ffmpeg_missing",
			Title:   "ffmpeg is required",
			Message: "Merging video and audio, converting formats and embedding subtitles all need ffmpeg, which wasn't found.",
			Hint:    "Install ffmpeg (for example: winget install Gyan.FFmpeg) or set its location under Settings → General.",
			Action:  ActionInstallFFmp,
		}
	}),
	r(`javascript runtime|js runtime|no supported javascript|challenge solving failed|n challenge`, func() AppError {
		return AppError{
			Code:    "js_runtime",
			Title:   "JavaScript runtime needed",
			Message: "YouTube now requires yt-dlp to run a small piece of JavaScript, and no supported runtime (deno, node, bun or quickjs) was found.",
			Hint:    "Install Deno or Node.js (20+), restart the app, and retry.",
			Action:  ActionSettings,
		}
	}),
	r(`invalid (filter|format) specification|invalid format|unknown format`, func() AppError {
		return AppError{
			Code:    "bad_format",
			Title:   "That format selection isn't valid",
			Message: "yt-dlp couldn't understand the format expression.",
			Hint:    "Pick a quality preset instead, or check the custom format against the yt-dlp format-selection syntax.",
			Action:  ActionNone,
		}
	}),
	r(`requested format is not available|no video formats found|format .* is not available|no formats? (found|available)`, func() AppError {
		return AppError{
			Code:    "format_unavailable",
			Title:   "That quality isn't available",
			Message: "The site doesn't offer a format matching your selection for this video.",
			Hint:    "Choose “Best available” or a lower resolution. If very few formats appear, update yt-dlp and make sure a JavaScript runtime is installed.",
			Action:  ActionUpdate,
		}
	}),
	r(`live event will begin|premieres in|this live event|will begin in`, func() AppError {
		return AppError{
			Code:      "upcoming",
			Title:     "Not live yet",
			Message:   "This is a scheduled live stream or premiere that hasn't started.",
			Hint:      "Retry once it has started.",
			Retryable: true,
		}
	}),
	r(`video unavailable|this video is unavailable|this video is not available|has been removed|no longer available|account.*terminated|video has been (deleted|taken down)|does not exist|http error 404`, func() AppError {
		return AppError{
			Code:    "unavailable",
			Title:   "Video unavailable",
			Message: "The video was removed, made private, or never existed at this address.",
			Hint:    "Open the link in a browser to confirm it still plays. If it only plays when you're signed in, use cookies.",
			Action:  ActionCookies,
		}
	}),
	r(`no space left|not enough space|disk full|winerror 112`, func() AppError {
		return AppError{
			Code:    "disk_full",
			Title:   "Disk is full",
			Message: "There isn't enough free space in the download folder.",
			Hint:    "Free some space or choose another download folder, then retry.",
			Action:  ActionSettings, Retryable: true,
		}
	}),
	r(`file name too long|filename too long|winerror 206|errno 36|path too long`, func() AppError {
		return AppError{
			Code:    "path_too_long",
			Title:   "File path too long",
			Message: "The resulting file name or folder path exceeds what Windows allows.",
			Hint:    "Choose a shorter download folder or simplify the filename template under Settings → General.",
			Action:  ActionSettings,
		}
	}),
	r(`permission denied|access is denied|winerror 5\b|errno 13|read-only file system`, func() AppError {
		return AppError{
			Code:    "permission",
			Title:   "Can't write to that folder",
			Message: "yt-dlp was denied permission to write to the download location.",
			Hint:    "Choose a folder you own (such as Downloads), or close any program using the file.",
			Action:  ActionSettings, Retryable: true,
		}
	}),
	r(`http error 40[13]|forbidden`, func() AppError {
		return AppError{
			Code:      "forbidden",
			Title:     "Access denied by the site",
			Message:   "The server refused the request (HTTP 401/403). This often means the site changed how it serves video.",
			Hint:      "Update yt-dlp first. If it still fails, try signed-in cookies.",
			Action:    ActionUpdate,
			Retryable: true,
		}
	}),
	r(`unable to extract|unable to download (webpage|json|video data)|signature extraction failed|nsig extraction failed|extractor error|please report this issue`, func() AppError {
		return AppError{
			Code:    "extractor_error",
			Title:   "yt-dlp couldn't read this page",
			Message: "The site probably changed its layout, which breaks the extractor in this yt-dlp version.",
			Hint:    "Update yt-dlp (consider the nightly channel) and retry.",
			Action:  ActionUpdate, Retryable: true,
		}
	}),
	r(`traceback \(most recent call last\)|failed to execute script|unhandled exception`, func() AppError {
		return AppError{
			Code:    "ytdlp_crash",
			Title:   "yt-dlp crashed",
			Message: "yt-dlp stopped with an internal error instead of a normal message.",
			Hint:    "Update yt-dlp and retry. If it keeps happening, open the details: they're what you'd include in a bug report.",
			Action:  ActionUpdate, Retryable: true,
		}
	}),
}

// Classify inspects yt-dlp's stderr lines (and the process error) and returns the most
// specific error it can recognise. It never returns nil.
func Classify(lines []string, runErr error) *AppError {
	var errLines, otherLines []string
	for _, l := range lines {
		switch l = strings.TrimSpace(l); {
		case strings.HasPrefix(l, "ERROR:"):
			errLines = append(errLines, l)
		case !strings.HasPrefix(l, "WARNING:"):
			otherLines = append(otherLines, l)
		}
	}
	// Without an ERROR: line (a crash prints a bare traceback) match everything except warnings:
	// a common warning such as "No supported JavaScript runtime" would otherwise hide the cause.
	haystack := strings.Join(errLines, "\n")
	detail := haystack
	if len(errLines) == 0 {
		haystack = strings.Join(otherLines, "\n")
		detail = strings.Join(lines, "\n")
	}
	if detail == "" && runErr != nil {
		detail = runErr.Error()
	}

	for _, rl := range rules {
		if rl.re.MatchString(haystack) {
			e := rl.build()
			e.Detail = detail
			return &e
		}
	}

	msg := lastMessage(errLines)
	if msg == "" {
		// No "ERROR:" line: fall back to the last line that isn't just a warning.
		for i := len(lines) - 1; i >= 0; i-- {
			if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "WARNING:") {
				msg = l
				if len(msg) > 400 {
					msg = msg[:400] + "…"
				}
				break
			}
		}
	}
	if msg == "" && runErr != nil {
		msg = runErr.Error()
	}
	if msg == "" {
		msg = "yt-dlp exited without saying why."
	}
	return &AppError{
		Code:      "unknown",
		Title:     "Download failed",
		Message:   msg,
		Hint:      "Open the details for the full output. Updating yt-dlp fixes many site breakages.",
		Action:    ActionUpdate,
		Detail:    detail,
		Retryable: true,
	}
}

func lastMessage(errLines []string) string {
	if len(errLines) == 0 {
		return ""
	}
	m := strings.TrimSpace(strings.TrimPrefix(errLines[len(errLines)-1], "ERROR:"))
	if len(m) > 400 {
		m = m[:400] + "…"
	}
	return m
}
