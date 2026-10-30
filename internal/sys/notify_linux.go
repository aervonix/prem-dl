//go:build linux

package sys

import (
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Desktop notifications over org.freedesktop.Notifications. Wails has its own, but they can't
// carry the app's icon or desktop entry, so the desktop showed them under the exe's file name.

const (
	notifyName  = "org.freedesktop.Notifications"
	notifyPath  = dbus.ObjectPath("/org/freedesktop/Notifications")
	notifyIface = "org.freedesktop.Notifications"

	// NotifyClicked is the action reported when the notification itself was clicked.
	NotifyClicked = "default"
)

// NotifyAction is a button on a notification.
type NotifyAction struct {
	ID    string
	Label string
}

// Note is one notification. Key comes back with whatever the user clicks.
type Note struct {
	Title   string
	Body    string
	Actions []NotifyAction
	Key     string
}

// Notifier sends notifications and reports clicks on them.
type Notifier struct {
	conn    *dbus.Conn
	name    string // the app's name, shown with them
	entry   string // its desktop entry, for the icon and settings the desktop keeps per app
	icon    string
	markup  bool // the server reads the body as markup, so it is escaped
	actions bool // the server shows buttons
	onClick func(action, key string)

	mu   sync.Mutex
	keys map[uint32]string // the notifications on screen
}

// StartNotifier connects to the desktop's notification server. entry names the app's desktop
// entry (prem-dl, for prem-dl.desktop) and icon an icon of the theme or a file. onClick runs on
// a goroutine of its own with the action (NotifyClicked for the notification itself) and the
// note's Key.
func StartNotifier(name, entry, icon string, onClick func(action, key string)) (*Notifier, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	n := &Notifier{conn: conn, name: name, entry: entry, icon: icon, onClick: onClick, keys: map[uint32]string{}}
	var caps []string
	if err := conn.Object(notifyName, notifyPath).Call(notifyIface+".GetCapabilities", 0).Store(&caps); err != nil {
		conn.Close()
		return nil, err
	}
	n.markup = slices.Contains(caps, "body-markup")
	n.actions = slices.Contains(caps, "actions")
	if err := conn.AddMatchSignal(dbus.WithMatchInterface(notifyIface), dbus.WithMatchObjectPath(notifyPath)); err != nil {
		conn.Close()
		return nil, err
	}
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	go n.listen(signals)
	return n, nil
}

func (n *Notifier) listen(signals <-chan *dbus.Signal) {
	for sig := range signals {
		if len(sig.Body) < 2 {
			continue
		}
		id, ok := sig.Body[0].(uint32)
		if !ok {
			continue
		}
		switch sig.Name {
		case notifyIface + ".ActionInvoked":
			action, _ := sig.Body[1].(string)
			n.mu.Lock()
			key, mine := n.keys[id]
			n.mu.Unlock()
			if mine && n.onClick != nil {
				go n.onClick(action, key)
			}
		case notifyIface + ".NotificationClosed":
			n.mu.Lock()
			delete(n.keys, id)
			n.mu.Unlock()
		}
	}
}

// Send shows a notification.
func (n *Notifier) Send(note Note) error {
	body := note.Body
	if n.markup {
		body = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(body)
	}
	actions := []string{NotifyClicked, "Open"}
	if n.actions {
		for _, a := range note.Actions {
			actions = append(actions, a.ID, a.Label)
		}
	}
	hints := map[string]dbus.Variant{
		"desktop-entry": dbus.MakeVariant(n.entry),
		"urgency":       dbus.MakeVariant(byte(1)),
	}
	var id uint32
	err := n.conn.Object(notifyName, notifyPath).Call(notifyIface+".Notify", 0,
		n.name, uint32(0), n.icon, note.Title, body, actions, hints, int32(-1)).Store(&id)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.keys[id] = note.Key
	n.mu.Unlock()
	return nil
}

// Close disconnects; clicks on notifications still on screen are no longer reported.
func (n *Notifier) Close() { n.conn.Close() }
