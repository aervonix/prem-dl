//go:build linux

package sys

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakeNotifications plays the desktop's notification server.
type fakeNotifications struct {
	conn *dbus.Conn
	caps []string
	sent chan sentNote
	next uint32
}

type sentNote struct {
	id                     uint32
	app, icon, title, body string
	actions                []string
	hints                  map[string]dbus.Variant
}

func (f *fakeNotifications) GetCapabilities() ([]string, *dbus.Error) { return f.caps, nil }

func (f *fakeNotifications) Notify(app string, replaces uint32, icon, title, body string, actions []string, hints map[string]dbus.Variant, timeout int32) (uint32, *dbus.Error) {
	f.next++
	f.sent <- sentNote{id: f.next, app: app, icon: icon, title: title, body: body, actions: actions, hints: hints}
	return f.next, nil
}

func startNotifications(t *testing.T, caps ...string) *fakeNotifications {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeNotifications{conn: conn, caps: caps, sent: make(chan sentNote, 4)}
	if err := conn.Export(f, notifyPath, notifyIface); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(notifyName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("notifications name: %v %v", reply, err)
	}
	t.Cleanup(func() { conn.Close() })
	return f
}

func (f *fakeNotifications) receive(t *testing.T) sentNote {
	t.Helper()
	select {
	case n := <-f.sent:
		return n
	case <-time.After(3 * time.Second):
		t.Fatal("no notification arrived")
		return sentNote{}
	}
}

func TestNotificationsAndClicks(t *testing.T) {
	privateBus(t)
	server := startNotifications(t, "actions", "body", "body-markup")
	type click struct{ action, key string }
	clicks := make(chan click, 4)
	n, err := StartNotifier("Prem-dl", "prem-dl", "prem-dl", func(action, key string) { clicks <- click{action, key} })
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()

	err = n.Send(Note{
		Title:   "Download finished",
		Body:    "Tom & Jerry <Remastered>",
		Actions: []NotifyAction{{"open", "Open"}, {"reveal", "Show in folder"}},
		Key:     "job-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := server.receive(t)
	if got.app != "Prem-dl" || got.icon != "prem-dl" || got.title != "Download finished" {
		t.Fatalf("sent %+v", got)
	}
	if got.body != "Tom &amp; Jerry &lt;Remastered&gt;" {
		t.Fatalf("body %q: the server reads markup, so it must be escaped", got.body)
	}
	if want := []string{"default", "Open", "open", "Open", "reveal", "Show in folder"}; !equalStrings(got.actions, want) {
		t.Fatalf("actions %v, want %v", got.actions, want)
	}
	if got.hints["desktop-entry"].Value() != "prem-dl" {
		t.Fatalf("hints %v", got.hints)
	}

	// The user clicks a button; a click on someone else's notification is ignored.
	_ = server.conn.Emit(notifyPath, notifyIface+".ActionInvoked", uint32(99), "open")
	_ = server.conn.Emit(notifyPath, notifyIface+".ActionInvoked", got.id, "reveal")
	select {
	case c := <-clicks:
		if c != (click{"reveal", "job-1"}) {
			t.Fatalf("click = %+v", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the click wasn't reported")
	}

	// Closed, it is forgotten.
	_ = server.conn.Emit(notifyPath, notifyIface+".NotificationClosed", got.id, uint32(2))
	deadline := time.Now().Add(2 * time.Second)
	for {
		n.mu.Lock()
		left := len(n.keys)
		n.mu.Unlock()
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a closed notification is still remembered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A server without buttons or markup gets plain text and only the click on the notification.
func TestNotificationsOnASimpleServer(t *testing.T) {
	privateBus(t)
	server := startNotifications(t, "body")
	n, err := StartNotifier("Prem-dl", "prem-dl", "prem-dl", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if err := n.Send(Note{Title: "x", Body: "a & b", Actions: []NotifyAction{{"open", "Open"}}}); err != nil {
		t.Fatal(err)
	}
	got := server.receive(t)
	if got.body != "a & b" || !equalStrings(got.actions, []string{"default", "Open"}) {
		t.Fatalf("sent %+v", got)
	}
}

func TestNoNotificationServer(t *testing.T) {
	privateBus(t)
	if n, err := StartNotifier("Prem-dl", "prem-dl", "prem-dl", nil); err == nil {
		n.Close()
		t.Fatal("started without a notification server")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
