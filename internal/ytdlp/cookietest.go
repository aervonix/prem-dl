package ytdlp

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/sys"
)

// CookieTest is the result of trying to load cookies from a browser.
type CookieTest struct {
	OK      bool             `json:"ok"`
	Count   int              `json:"count"`
	Browser string           `json:"browser"`
	Error   *apperr.AppError `json:"error"`
}

var extractedRe = regexp.MustCompile(`Extracted (\d+) cookies? from (\S+)`)

// TestBrowserCookies asks yt-dlp to load cookies from the configured browser. It aims yt-dlp at a
// closed local port, so the cookies are decrypted into memory but never sent to any website.
func TestBrowserCookies(ctx context.Context, env Env) CookieTest {
	s := env.Settings.Cookies
	res := CookieTest{Browser: s.Browser}
	spec := s.Browser
	if s.Profile != "" {
		spec += ":" + s.Profile
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, env.Bin,
		"--ignore-config", "--encoding", "utf-8", "--color", "never",
		"--cookies-from-browser="+spec,
		"--simulate", "--retries", "0", "--socket-timeout", "3",
		"--", "http://127.0.0.1:1/")
	sys.Quiet(cmd)
	cmd.Env = childEnv()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	runErr := cmd.Run()

	if m := extractedRe.FindStringSubmatch(out.String()); m != nil {
		res.Count, _ = strconv.Atoi(m[1])
		res.OK = res.Count > 0
		if !res.OK {
			res.Error = apperr.New("cookie_empty", "No cookies found",
				"The browser profile was readable but contains no cookies.",
				"Sign in to the site in that browser first, or choose another profile.", apperr.ActionCookies)
		}
		return res
	}
	if ctx.Err() != nil {
		res.Error = apperr.New("timeout", "Cookie check timed out",
			"Reading the browser's cookies took too long.", "Close the browser and try again.", apperr.ActionCookies)
		return res
	}
	res.Error = apperr.Classify(splitLines(out.String()), runErr)
	return res
}
