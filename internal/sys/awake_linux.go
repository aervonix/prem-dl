//go:build linux

package sys

import (
	"syscall"

	"github.com/godbus/dbus/v5"
)

// holdAwake takes a logind inhibitor lock against sleep, as systemd-inhibit does. It lasts while
// the file descriptor logind hands back is open. Closing the lid still suspends (logind ignores
// inhibitors for the lid by default).
func holdAwake() (func(), error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	var fd dbus.UnixFD
	err = conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		Call("org.freedesktop.login1.Manager.Inhibit", 0, "sleep", "Prem-dl", "Downloading", "block").Store(&fd)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Close(int(fd))
		conn.Close()
	}, nil
}
