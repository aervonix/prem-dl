package apperr

import (
	"errors"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		lines  []string
		code   string
		action string
	}{
		{
			name:   "dpapi (observed on this machine)",
			lines:  []string{"Extracting cookies from edge", "ERROR: Failed to decrypt with DPAPI. See  https://github.com/yt-dlp/yt-dlp/issues/10927  for more info"},
			code:   "cookie_decrypt",
			action: ActionCookies,
		},
		{
			name:   "firefox missing (observed)",
			lines:  []string{`ERROR: could not find firefox cookies database in 'C:\\Users\\x\\AppData\\Roaming\\Mozilla\\Firefox\\Profiles'`},
			code:   "cookie_browser_missing",
			action: ActionCookies,
		},
		{
			name:   "unavailable video (observed)",
			lines:  []string{"WARNING: [youtube] No supported JavaScript runtime could be found.", "ERROR: [youtube] jNQXAC9IeRw: This video is unavailable"},
			code:   "unavailable",
			action: ActionCookies,
		},
		{
			name:   "connection refused (observed)",
			lines:  []string{"ERROR: [generic] 127.0.0: Unable to download webpage: HTTPConnection(host='127.0.0.1', port=1): Failed to establish a new connection: [WinError 10061] No connection could be made because the target machine actively refused it"},
			code:   "network",
			action: ActionSettings,
		},
		{
			name:   "bot check with curly apostrophe",
			lines:  []string{"ERROR: [youtube] abc: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication."},
			code:   "bot_check",
			action: ActionCookies,
		},
		{
			name:   "age gate",
			lines:  []string{"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users."},
			code:   "age_restricted",
			action: ActionCookies,
		},
		{
			name:   "private",
			lines:  []string{"ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video"},
			code:   "private",
			action: ActionCookies,
		},
		{
			name:   "geo",
			lines:  []string{"ERROR: [youtube] abc: The uploader has not made this video available in your country"},
			code:   "geo_blocked",
			action: ActionSettings,
		},
		{
			name:   "429",
			lines:  []string{"ERROR: unable to download video data: HTTP Error 429: Too Many Requests"},
			code:   "rate_limited",
			action: ActionSettings,
		},
		{
			name:   "403",
			lines:  []string{"ERROR: unable to download video data: HTTP Error 403: Forbidden"},
			code:   "forbidden",
			action: ActionUpdate,
		},
		{
			name:   "unsupported",
			lines:  []string{"ERROR: Unsupported URL: https://example.com/"},
			code:   "unsupported_url",
			action: ActionUpdate,
		},
		{
			name:   "ffmpeg",
			lines:  []string{"ERROR: Postprocessing: ffprobe and ffmpeg not found. Please install or provide the path using --ffmpeg-location"},
			code:   "ffmpeg_missing",
			action: ActionInstallFFmp,
		},
		{
			name:   "format",
			lines:  []string{"ERROR: [youtube] abc: Requested format is not available. Use --list-formats for a list of available formats"},
			code:   "format_unavailable",
			action: ActionUpdate,
		},
		{
			name:   "js runtime",
			lines:  []string{"ERROR: [youtube] abc: n challenge solving failed: Some formats may be missing."},
			code:   "js_runtime",
			action: ActionSettings,
		},
		{
			name:   "disk",
			lines:  []string{"ERROR: unable to write data: [Errno 28] No space left on device"},
			code:   "disk_full",
			action: ActionSettings,
		},
		{
			name:   "extractor",
			lines:  []string{"ERROR: [foo] 123: Unable to extract video data; please report this issue on https://github.com/yt-dlp/yt-dlp/issues"},
			code:   "extractor_error",
			action: ActionUpdate,
		},
		{
			name:   "bad custom format (observed: the traceback that exposed a real bug)",
			lines:  []string{`Traceback (most recent call last): File "yt_dlp\YoutubeDL.py", line 2261, in _build_format_filter SyntaxError: Invalid filter specification 'height<=360?' [PYI-15736:ERROR] Failed to execute script '__main__' due to unhandled exception!`},
			code:   "bad_format",
			action: ActionNone,
		},
		{
			name:   "generic crash",
			lines:  []string{`Traceback (most recent call last): File "x.py", line 1, in <module> KeyError: 'foo'`},
			code:   "ytdlp_crash",
			action: ActionUpdate,
		},
		{
			name:   "invalid url from yt-dlp",
			lines:  []string{"yt-dlp: error: You must provide at least one URL."},
			code:   "invalid_url",
			action: ActionNone,
		},
		{
			name:   "unknown falls back with last message",
			lines:  []string{"ERROR: something nobody has seen before"},
			code:   "unknown",
			action: ActionUpdate,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.lines, nil)
			if got.Code != c.code {
				t.Fatalf("code = %q, want %q (message: %s)", got.Code, c.code, got.Message)
			}
			if got.Action != c.action {
				t.Errorf("action = %q, want %q", got.Action, c.action)
			}
			if got.Title == "" || got.Message == "" {
				t.Errorf("title/message must never be empty: %+v", got)
			}
		})
	}
}

func TestClassifyUnknownKeepsOriginalMessage(t *testing.T) {
	got := Classify([]string{"ERROR: something nobody has seen before"}, nil)
	if got.Message != "something nobody has seen before" {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestClassifyNeverNil(t *testing.T) {
	if Classify(nil, nil) == nil {
		t.Fatal("nil with no input")
	}
	got := Classify(nil, errors.New("exit status 1"))
	if got.Message != "exit status 1" {
		t.Fatalf("message = %q", got.Message)
	}
}

func TestOnlyErrorLinesDecideTheCode(t *testing.T) {
	// A warning mentioning "sign in" must not override the real error.
	got := Classify([]string{
		"WARNING: please sign in for better quality",
		"ERROR: [youtube] abc: Video unavailable",
	}, nil)
	if got.Code != "unavailable" {
		t.Fatalf("code = %q", got.Code)
	}
}

func TestWarningsDoNotDecideACrash(t *testing.T) {
	// No ERROR: line at all: a crash prints a bare traceback, usually after routine warnings.
	cases := []struct {
		name, warning, crash, code string
	}{
		{"js runtime warning", "WARNING: [youtube] No supported JavaScript runtime could be found.",
			`Traceback (most recent call last): File "yt_dlp\YoutubeDL.py", line 2261, in _build_format_filter SyntaxError: Invalid filter specification 'height<=360?'`, "bad_format"},
		{"timeout warning", "WARNING: [youtube] abc: Read timed out. Retrying (1/3)...",
			`Traceback (most recent call last): File "x.py", line 1, in <module> KeyError: 'foo'`, "ytdlp_crash"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify([]string{c.warning, c.crash}, nil)
			if got.Code != c.code {
				t.Fatalf("code = %q, want %q", got.Code, c.code)
			}
			if !strings.Contains(got.Detail, c.warning) {
				t.Errorf("details should still show the warning: %q", got.Detail)
			}
		})
	}
}
