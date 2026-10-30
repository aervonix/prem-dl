//go:build !linux

package main

import (
	"context"
	"os"
	"path/filepath"

	wr "github.com/wailsapp/wails/v2/pkg/runtime"

	"yt-dlp-gui/internal/sys"
)

// Notifications through Wails, which on Windows are toasts.

func (a *App) initNotifications(ctx context.Context) {
	if exe, err := os.Executable(); err == nil {
		// Wails registers the app under the exe's file name, which Windows would show as its name.
		_ = sys.SetToastName(filepath.Base(exe), "Prem-dl")
	}
	if err := wr.InitializeNotifications(ctx); err != nil {
		wr.LogWarning(ctx, "notifications unavailable: "+err.Error())
		return
	}
	for _, id := range []string{toastDone, toastFailed} {
		_ = wr.RegisterNotificationCategory(ctx, wr.NotificationCategory{ID: id, Actions: toastActions[id]})
	}
	wr.OnNotificationResponse(ctx, a.onNotification)
	a.toasts.Store(true)
}

func (a *App) sendNotification(ctx context.Context, n wr.NotificationOptions) error {
	if n.CategoryID != "" {
		return wr.SendNotificationWithActions(ctx, n)
	}
	return wr.SendNotification(ctx, n)
}
