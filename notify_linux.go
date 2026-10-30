package main

import (
	"context"
	"errors"
	"sync/atomic"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/sys"
)

// Notifications straight over D-Bus rather than through Wails, so they carry the app's name,
// icon and desktop entry (see sys.Notifier).

var notifier atomic.Pointer[sys.Notifier]

func (a *App) initNotifications(ctx context.Context) {
	n, err := sys.StartNotifier("Prem-dl", urlScheme, urlScheme, func(action, job string) {
		a.onNotification(wr.NotificationResult{Response: wr.NotificationResponse{
			ActionIdentifier: action,
			UserInfo:         map[string]any{"job": job},
		}})
	})
	if err != nil {
		wr.LogWarning(ctx, "notifications unavailable: "+err.Error())
		return
	}
	notifier.Store(n)
	a.toasts.Store(true)
}

func (a *App) sendNotification(_ context.Context, n wr.NotificationOptions) error {
	nt := notifier.Load()
	if nt == nil {
		return errors.New("notifications aren't available")
	}
	note := sys.Note{Title: n.Title, Body: n.Body}
	note.Key, _ = n.Data["job"].(string)
	for _, act := range toastActions[n.CategoryID] {
		note.Actions = append(note.Actions, sys.NotifyAction{ID: act.ID, Label: act.Title})
	}
	return nt.Send(note)
}
