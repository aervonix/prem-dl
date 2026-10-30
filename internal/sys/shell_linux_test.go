//go:build linux

package sys

import (
	"bufio"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// privateBus starts a session bus of the test's own and points the process at it.
func privateBus(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon isn't installed")
	}
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address",
		"--address=unix:path="+filepath.Join(t.TempDir(), "bus"))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(addr))
}

// fakeTray plays org.kde.StatusNotifierWatcher and records who registers.
type fakeTray struct {
	conn       *dbus.Conn
	registered chan string
}

func (f *fakeTray) RegisterStatusNotifierItem(service string) *dbus.Error {
	f.registered <- service
	return nil
}

func startTray(t *testing.T) *fakeTray {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeTray{conn: conn, registered: make(chan string, 4)}
	if err := conn.Export(f, watcherPath, watcherName); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("watcher name: %v %v", reply, err)
	}
	t.Cleanup(func() { conn.Close() })
	return f
}

func (f *fakeTray) waitRegistered(t *testing.T) string {
	t.Helper()
	select {
	case name := <-f.registered:
		return name
	case <-time.After(3 * time.Second):
		t.Fatal("the icon didn't register with the tray")
		return ""
	}
}

func testIcon(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// menuState is what trayMenu returns, changeable by the test.
type menuState struct {
	mu    sync.Mutex
	items []MenuItem
}

func (m *menuState) get() []MenuItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slicesClone(m.items)
}

func (m *menuState) set(items []MenuItem) {
	m.mu.Lock()
	m.items = items
	m.mu.Unlock()
}

func slicesClone(items []MenuItem) []MenuItem { return append([]MenuItem(nil), items...) }

func TestTrayIconAndMenu(t *testing.T) {
	privateBus(t)
	tray := startTray(t)
	menu := &menuState{items: []MenuItem{
		{ID: 1, Label: "Open Prem-dl", Default: true},
		{},
		{ID: 2, Label: "Pause all", Disabled: true},
		{ID: 5, Label: "Quit Prem-dl"},
	}}
	opened := make(chan struct{}, 1)
	chosen := make(chan int, 4)
	sh, err := StartShell(ShellOptions{
		OnOpen: func() { opened <- struct{}{} },
		Menu:   menu.get,
		OnMenu: func(id int) { chosen <- id },
		Icon:   testIcon(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	name := tray.waitRegistered(t)

	item := tray.conn.Object(name, itemPath)
	status := func() string {
		v, err := item.GetProperty(itemIface + ".Status")
		if err != nil {
			t.Fatal(err)
		}
		return v.Value().(string)
	}
	if s := status(); s != "Passive" {
		t.Fatalf("a new icon is %s; it shows only when asked", s)
	}

	// Shown, with a tooltip whose first line is its title.
	sh.SetTray(true, "Prem-dl\n2 downloading")
	if s := status(); s != "Active" {
		t.Fatalf("status = %s", s)
	}
	var tip toolTip
	if v, err := item.GetProperty(itemIface + ".ToolTip"); err != nil || dbus.Store([]any{v.Value()}, &tip) != nil {
		t.Fatalf("tooltip: %v %v", v, err)
	}
	if tip.Title != "Prem-dl" || tip.Body != "2 downloading" {
		t.Fatalf("tooltip = %+v", tip)
	}

	// The icon comes in every size, as ARGB.
	var pixmaps []pixmap
	if v, err := item.GetProperty(itemIface + ".IconPixmap"); err != nil || dbus.Store([]any{v.Value()}, &pixmaps) != nil {
		t.Fatalf("pixmaps: %v", err)
	}
	if len(pixmaps) != len(iconSizes) {
		t.Fatalf("%d sizes", len(pixmaps))
	}
	for _, p := range pixmaps {
		if len(p.Data) != int(p.W*p.H*4) || p.Data[0] != 0xff {
			t.Fatalf("pixmap %dx%d holds %d bytes, first %x", p.W, p.H, len(p.Data), p.Data[0])
		}
	}

	// The menu as the tray reads it.
	m := tray.conn.Object(name, menuPath)
	var rev uint32
	var layout menuLayout
	if err := m.Call(menuIface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&rev, &layout); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range layout.Children {
		var child menuLayout
		if err := dbus.Store([]any{c.Value()}, &child); err != nil {
			t.Fatal(err)
		}
		switch {
		case child.Props["type"].Value() == "separator":
			got = append(got, "---")
		case child.Props["enabled"].Value() == false:
			got = append(got, child.Props["label"].Value().(string)+" (off)")
		default:
			got = append(got, child.Props["label"].Value().(string))
		}
	}
	if s := strings.Join(got, " | "); s != "Open Prem-dl | --- | Pause all (off) | Quit Prem-dl" {
		t.Fatalf("menu = %s", s)
	}

	// Clicks: the icon opens the window, items run, disabled ones don't.
	if err := item.Call(itemIface+".Activate", 0, int32(0), int32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("clicking the icon didn't open the window")
	}
	for _, id := range []int32{2, 5} {
		if err := m.Call(menuIface+".Event", 0, id, "clicked", dbus.MakeVariant(""), uint32(0)).Err; err != nil {
			t.Fatal(err)
		}
	}
	select {
	case id := <-chosen:
		if id != 5 {
			t.Fatalf("chose %d; the disabled item must do nothing", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("choosing Quit did nothing")
	}

	// The menu follows the queue: opening it builds it again.
	menu.set([]MenuItem{{ID: 1, Label: "Open Prem-dl"}, {ID: 2, Label: "Pause all"}})
	var needUpdate bool
	if err := m.Call(menuIface+".AboutToShow", 0, int32(0)).Store(&needUpdate); err != nil || !needUpdate {
		t.Fatalf("AboutToShow = %v %v", needUpdate, err)
	}
	var rev2 uint32
	if err := m.Call(menuIface+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&rev2, &layout); err != nil {
		t.Fatal(err)
	}
	if rev2 == rev || len(layout.Children) != 2 {
		t.Fatalf("revision %d → %d, %d items", rev, rev2, len(layout.Children))
	}
	if err := m.Call(menuIface+".AboutToShow", 0, int32(0)).Store(&needUpdate); err != nil || needUpdate {
		t.Fatalf("an unchanged menu needs an update: %v %v", needUpdate, err)
	}
}

func TestTrayWaitsForTheDesktopsTray(t *testing.T) {
	privateBus(t)
	old := watcherWait
	defer func() { watcherWait = old }()

	watcherWait = 300 * time.Millisecond
	if sh, err := StartShell(ShellOptions{}); err == nil {
		sh.Close()
		t.Fatal("an icon without a tray")
	}

	// Started at sign-in, before the tray: it waits for it.
	watcherWait = 5 * time.Second
	trays := make(chan *fakeTray, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		trays <- startTray(t)
	}()
	sh, err := StartShell(ShellOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	(<-trays).waitRegistered(t)
}

func TestTrayRegistersAgainWhenTheTrayRestarts(t *testing.T) {
	privateBus(t)
	first := startTray(t)
	sh, err := StartShell(ShellOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Close()
	name := first.waitRegistered(t)

	first.conn.Close() // GNOME Shell restarting
	second := startTray(t)
	if again := second.waitRegistered(t); again != name {
		t.Fatalf("registered as %s, then %s", name, again)
	}
}

func TestScaleARGBKeepsColourAtTransparentEdges(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0xff, A: 0xff}) // one opaque red pixel, three transparent
	px := scaleARGB(img, 1)
	if px[0] < 0x3f || px[0] > 0x40 || px[1] != 0xff || px[2] != 0 || px[3] != 0 {
		t.Fatalf("ARGB = %x, want a quarter-opaque full red", px)
	}
}
