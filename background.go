package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/cookiebridge"
	"yt-dlp-gui/internal/linkbridge"
	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/sys"
	"yt-dlp-gui/internal/ytdlp"
)

// Everything that works while the window is in the background or hidden: the tray icon, the
// taskbar button's progress, Windows notifications, starting with Windows, and links sent from
// the Chrome helper.

// runName names the entry that starts the app when the user signs in to Windows.
const runName = "Prem-dl"

const (
	menuOpen = iota + 1
	menuPauseAll
	menuResumeAll
	menuCheckFeeds
	menuQuit
)

// Notification categories: which buttons a notification has.
const (
	toastDone   = "download-done"
	toastFailed = "download-failed"
)

var toastActions = map[string][]wr.NotificationAction{
	toastDone:   {{ID: "open", Title: "Open"}, {ID: "reveal", Title: "Show in folder"}},
	toastFailed: {{ID: "retry", Title: "Retry"}},
}

// startBackground runs once at startup, after the window exists.
func (a *App) startBackground(ctx context.Context) {
	sh, err := sys.StartShell(sys.ShellOptions{OnOpen: a.showWindow, Menu: a.trayMenu, OnMenu: a.onTrayMenu, Icon: appIcon})
	if err != nil {
		wr.LogWarning(ctx, "tray icon unavailable: "+err.Error())
		a.noTray.Store(true)
		a.emit("app:no-tray", true)
		if a.hidden.Load() {
			a.showWindow() // started in the tray, but there is none: nothing could show the window
		}
	} else {
		a.shell.Store(sh)
		a.updateShell(a.act.current())
	}
	a.startLinkBridge()
	a.registerWithSystem()
	a.initNotifications(ctx)
	// A helper that was set up before gets the current files, ready for Reload in Chrome.
	if _, err := os.Stat(a.chromeHelperDir()); err == nil {
		_, _ = a.writeChromeHelper()
	}
}

// updateShell shows ov on the taskbar button and in the tray icon's tooltip. The icon is there
// while the window is hidden, and always when closing is set to keep the app in the tray.
func (a *App) updateShell(ov overview) {
	sh := a.shell.Load()
	if sh == nil {
		return
	}
	sh.SetProgress(taskbarProgress(ov))
	sh.SetTray(a.hidden.Load() || a.store.Get().CloseToTray, trayTooltip(ov))
}

func (a *App) trayMenu() []sys.MenuItem {
	ov := a.act.current()
	return []sys.MenuItem{
		{ID: menuOpen, Label: "Open Prem-dl", Default: true},
		{},
		{ID: menuPauseAll, Label: "Pause all", Disabled: ov.Downloading+ov.Queued == 0},
		{ID: menuResumeAll, Label: "Resume all", Disabled: ov.Paused == 0},
		{ID: menuCheckFeeds, Label: "Check followed channels", Disabled: len(a.feeds.List()) == 0},
		{},
		{ID: menuQuit, Label: "Quit Prem-dl"},
	}
}

func (a *App) onTrayMenu(id int) {
	switch id {
	case menuOpen:
		a.showWindow()
	case menuPauseAll:
		a.q.PauseAll()
	case menuResumeAll:
		a.q.ResumeAll()
	case menuCheckFeeds:
		_ = a.feeds.CheckNow("")
	case menuQuit:
		a.quitFromTray()
	}
}

// showWindow brings the window back from the tray, the taskbar or behind other windows.
func (a *App) showWindow() {
	ctx := a.rt()
	if ctx == nil {
		return
	}
	a.hidden.Store(false)
	wr.WindowShow(ctx)
	// Windows only lets the foreground app hand out focus, so a link sent from Chrome may not
	// activate the window; this at least puts it on top.
	sys.RaiseMainWindow()
	if sh := a.shell.Load(); sh != nil {
		sh.Refresh() // the taskbar button is new and has no progress yet
	}
	a.updateShell(a.act.current())
}

// hideToTray hides the window, leaving the tray icon to bring it back. Without a tray icon
// there would be no way back, so then it does nothing and reports false.
func (a *App) hideToTray() bool {
	ctx := a.rt()
	if ctx == nil || a.shell.Load() == nil {
		return false
	}
	a.saveWindow()
	a.hidden.Store(true)
	a.updateShell(a.act.current()) // the icon first, so there's never neither
	wr.WindowHide(ctx)
	return true
}

// HideWindow is the close dialog's "Keep downloading in the background".
func (a *App) HideWindow() {
	a.CancelClose()
	a.hideToTray()
}

// quitFromTray quits for real, even when closing the window would only hide it. If downloads
// are running the window comes back to ask first.
func (a *App) quitFromTray() {
	ctx := a.rt()
	if ctx == nil {
		return
	}
	a.quitting.Store(true)
	if a.q.ActiveCount() > 0 {
		a.showWindow()
	}
	wr.Quit(ctx)
}

// showPage brings the window forward on a page.
func (a *App) showPage(page string) {
	a.showWindow()
	a.emit("app:navigate", page)
}

// ---- links from the Chrome helper ----

func (a *App) startLinkBridge() {
	ln, err := linkbridge.Listen()
	if err != nil {
		// The helper falls back to the prem-dl: link type, which reaches this instance too.
		wr.LogWarning(a.rt(), "no port free for the Chrome helper: "+err.Error())
		return
	}
	srv := &http.Server{
		Handler:           &linkbridge.Server{Origin: "chrome-extension://" + cookiebridge.ExtensionID, Open: a.openLink},
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		<-a.root.Done()
		_ = srv.Close()
	}()
}

// openLink puts a link on the Download page and brings the window forward to review it.
func (a *App) openLink(raw string) error {
	link, err := ytdlp.NormalizeURL(raw)
	if err != nil {
		return err
	}
	a.showWindow()
	a.emit("app:open-url", link)
	return nil
}

// ---- registration ----

// startCommand is what starting at sign-in runs.
func startCommand(exe string) string { return sys.CommandLine(exe, backgroundFlag) }

// registerWithSystem points the prem-dl: link type, and the sign-in entry when it's on, at this
// exe, in case it moved (the registry on Windows, desktop entries on Linux). Dev builds leave
// both alone so they never take over from the real app.
func (a *App) registerWithSystem() {
	if devBuild {
		return
	}
	exe, err := sys.AppPath()
	if err != nil {
		return
	}
	if err := installDesktopIcon(); err != nil {
		wr.LogWarning(a.rt(), "couldn't install the icon: "+err.Error())
	}
	if sys.URLSchemeCommand(urlScheme) != sys.SchemeCommand(exe) {
		if err := sys.RegisterURLScheme(urlScheme, "Prem-dl", exe); err != nil {
			wr.LogWarning(a.rt(), "couldn't register prem-dl: links: "+err.Error())
		}
	}
	if a.store.Get().StartWithWindows && sys.RunAtSignIn(runName) != startCommand(exe) {
		_ = sys.SetRunAtSignIn(runName, startCommand(exe))
	}
}

// setStartWithWindows adds or removes the sign-in entry (not in dev builds; see above).
func setStartWithWindows(on bool) error {
	if devBuild {
		return nil
	}
	if !on {
		return sys.SetRunAtSignIn(runName, "")
	}
	exe, err := sys.AppPath()
	if err != nil {
		return err
	}
	return sys.SetRunAtSignIn(runName, startCommand(exe))
}

// ---- notifications ----

// SetWindowFocused is the page saying the window gained or lost the keyboard focus. Linux can't
// be asked which window has it; on Windows, MainWindowInFront asks Windows and this does nothing.
func (a *App) SetWindowFocused(focused bool) { sys.SetMainWindowFocused(focused) }

// notifyFinished shows a Windows notification for downloads that just finished, unless the
// window is in front: the page shows its own card then.
func (a *App) notifyFinished(jobs []queue.Job) {
	ctx := a.rt()
	if ctx == nil || !a.toasts.Load() || !a.store.Get().NotifySystem {
		return
	}
	if !a.hidden.Load() && sys.MainWindowInFront() {
		return
	}
	if err := a.sendNotification(ctx, finishToast(jobs)); err != nil {
		wr.LogWarning(ctx, "notification failed: "+err.Error())
	}
}

// finishToast describes finished downloads: one gets its title and buttons, several a count.
func finishToast(jobs []queue.Job) wr.NotificationOptions {
	failed := 0
	for _, j := range jobs {
		if j.Status == queue.StatusFailed {
			failed++
		}
	}
	if len(jobs) == 1 {
		j := jobs[0]
		name := j.Title
		if name == "" {
			name = j.URL
		}
		n := wr.NotificationOptions{ID: j.ID, Body: name, Data: map[string]any{"job": j.ID}}
		if failed == 0 {
			n.Title = "Download finished"
			if j.OutputFile != "" {
				n.CategoryID = toastDone
			}
			return n
		}
		n.Title = "Download failed"
		n.CategoryID = toastFailed
		if e := j.Error; e != nil {
			why := e.Title
			if why == n.Title || why == "" {
				why = e.Message // the catch-all error's title says no more than ours
			}
			if r := []rune(why); len(r) > 120 {
				why = string(r[:119]) + "…"
			}
			if why != "" {
				n.Body = name + "\n" + why
			}
		}
		return n
	}
	n := wr.NotificationOptions{ID: jobs[0].ID, Title: fmt.Sprintf("%d downloads finished", len(jobs))}
	done := len(jobs) - failed
	switch {
	case failed == 0:
		n.Body = "All done."
	case done == 0:
		n.Title = fmt.Sprintf("%d downloads failed", failed)
		n.Body = "Open the queue to see what went wrong."
	default:
		n.Title = "Downloads finished with problems"
		n.Body = fmt.Sprintf("%d done, %d failed.", done, failed)
	}
	return n
}

// onNotification handles a click on a notification or one of its buttons.
func (a *App) onNotification(res wr.NotificationResult) {
	if res.Error != nil {
		return
	}
	id, _ := res.Response.UserInfo["job"].(string)
	var err error
	switch res.Response.ActionIdentifier {
	case "open":
		err = a.OpenFile(id)
	case "reveal":
		err = a.RevealFile(id)
	case "retry":
		err = a.RetryJob(id)
	default:
		a.showPage("queue")
		return
	}
	if err != nil {
		// The file was moved, or the job removed since: the queue shows what's left.
		a.showPage("queue")
		a.emit("app:error", err.Error())
	}
}
