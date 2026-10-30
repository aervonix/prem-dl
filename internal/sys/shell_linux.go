//go:build linux

package sys

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/png" // the icon
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// The tray icon is a StatusNotifierItem: an object on the session bus that the desktop's tray
// (KDE, or GNOME with the AppIndicator extension) shows, registered with the
// org.kde.StatusNotifierWatcher. Its menu is a com.canonical.dbusmenu object the tray draws.
// There is no taskbar progress: Linux has no standard for it.

const (
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = "/StatusNotifierWatcher"
	itemIface    = "org.kde.StatusNotifierItem"
	itemPath     = dbus.ObjectPath("/StatusNotifierItem")
	menuIface    = "com.canonical.dbusmenu"
	menuPath     = dbus.ObjectPath("/MenuBar")
	separatorIDs = 1000 // dbusmenu IDs of separators start here; items keep their own
)

// watcherWait is how long StartShell waits for the tray to appear. Started at sign-in, the app
// may be quicker than the desktop's tray.
var watcherWait = 5 * time.Second

// Shell owns the tray icon.
type Shell struct {
	opts  ShellOptions
	conn  *dbus.Conn
	name  string
	props *prop.Properties

	mu       sync.Mutex
	visible  bool
	tooltip  string
	items    []MenuItem // the menu as last built
	revision uint32
	closed   bool
}

// StartShell puts the icon on the session bus and registers it with the tray. It fails when the
// desktop has no tray (GNOME without the AppIndicator extension); the app then has no icon.
func StartShell(opts ShellOptions) (*Shell, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	s := &Shell{opts: opts, conn: conn, name: fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())}
	if err := s.export(); err != nil {
		conn.Close()
		return nil, err
	}
	if !s.waitForWatcher() {
		conn.Close()
		return nil, errors.New("the desktop has no tray (GNOME needs the AppIndicator extension)")
	}
	if err := s.register(); err != nil {
		conn.Close()
		return nil, err
	}
	s.rebuildMenu()
	return s, nil
}

func (s *Shell) export() error {
	reply, err := s.conn.RequestName(s.name, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("the bus name %s is taken", s.name)
	}
	pixmaps := iconPixmaps(s.opts.Icon)
	s.props, err = prop.Export(s.conn, itemPath, prop.Map{
		itemIface: {
			"Category":            {Value: "ApplicationStatus"},
			"Id":                  {Value: "prem-dl"},
			"Title":               {Value: "Prem-dl"},
			"Status":              {Value: "Passive", Emit: prop.EmitTrue},
			"WindowId":            {Value: int32(0)},
			"IconName":            {Value: ""},
			"IconThemePath":       {Value: ""},
			"IconPixmap":          {Value: pixmaps},
			"OverlayIconName":     {Value: ""},
			"OverlayIconPixmap":   {Value: []pixmap{}},
			"AttentionIconName":   {Value: ""},
			"AttentionIconPixmap": {Value: []pixmap{}},
			"AttentionMovieName":  {Value: ""},
			"ToolTip":             {Value: toolTip{Title: "Prem-dl", Icons: []pixmap{}}, Emit: prop.EmitTrue},
			"ItemIsMenu":          {Value: false},
			"Menu":                {Value: menuPath},
		},
	})
	if err != nil {
		return err
	}
	menuProps, err := prop.Export(s.conn, menuPath, prop.Map{
		menuIface: {
			"Version":       {Value: uint32(3)},
			"TextDirection": {Value: "ltr"},
			"Status":        {Value: "normal"},
			"IconThemePath": {Value: []string{}},
		},
	})
	if err != nil {
		return err
	}
	item, menu := (*sniItem)(s), (*sniMenu)(s)
	if err := s.conn.Export(item, itemPath, itemIface); err != nil {
		return err
	}
	if err := s.conn.Export(menu, menuPath, menuIface); err != nil {
		return err
	}
	if err := s.conn.Export(introspect.NewIntrospectable(&introspect.Node{
		Name: string(itemPath),
		Interfaces: []introspect.Interface{introspect.IntrospectData, prop.IntrospectData, {
			Name:       itemIface,
			Methods:    introspect.Methods(item),
			Properties: s.props.Introspection(itemIface),
			Signals: []introspect.Signal{
				{Name: "NewTitle"}, {Name: "NewIcon"}, {Name: "NewAttentionIcon"}, {Name: "NewOverlayIcon"},
				{Name: "NewToolTip"}, {Name: "NewStatus", Args: []introspect.Arg{{Name: "status", Type: "s"}}},
			},
		}},
	}), itemPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return err
	}
	return s.conn.Export(introspect.NewIntrospectable(&introspect.Node{
		Name: string(menuPath),
		Interfaces: []introspect.Interface{introspect.IntrospectData, prop.IntrospectData, {
			Name:       menuIface,
			Methods:    introspect.Methods(menu),
			Properties: menuProps.Introspection(menuIface),
			Signals: []introspect.Signal{
				{Name: "ItemsPropertiesUpdated", Args: []introspect.Arg{{Name: "updatedProps", Type: "a(ia{sv})"}, {Name: "removedProps", Type: "a(ias)"}}},
				{Name: "LayoutUpdated", Args: []introspect.Arg{{Name: "revision", Type: "u"}, {Name: "parent", Type: "i"}}},
				{Name: "ItemActivationRequested", Args: []introspect.Arg{{Name: "id", Type: "i"}, {Name: "timestamp", Type: "u"}}},
			},
		}},
	}), menuPath, "org.freedesktop.DBus.Introspectable")
}

// waitForWatcher reports whether the tray is there, waiting up to watcherWait for it. Once it
// is, a goroutine registers the icon again whenever the tray restarts (GNOME Shell restarting,
// the extension turned off and on).
func (s *Shell) waitForWatcher() bool {
	_ = s.conn.AddMatchSignal(
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, watcherName),
	)
	signals := make(chan *dbus.Signal, 8)
	s.conn.Signal(signals)
	appeared := make(chan struct{}, 1)
	go func() {
		for sig := range signals {
			if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) < 3 {
				continue
			}
			if owner, _ := sig.Body[2].(string); owner != "" {
				select {
				case appeared <- struct{}{}:
				default:
				}
			}
		}
	}()

	var has bool
	if err := s.conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, watcherName).Store(&has); err == nil && has {
		go s.reRegister(appeared)
		return true
	}
	select {
	case <-appeared:
		go s.reRegister(appeared)
		return true
	case <-time.After(watcherWait):
		return false
	}
}

func (s *Shell) reRegister(appeared <-chan struct{}) {
	for range appeared {
		if s.isClosed() {
			return
		}
		_ = s.register()
	}
}

func (s *Shell) register() error {
	return s.conn.Object(watcherName, watcherPath).Call(watcherName+".RegisterStatusNotifierItem", 0, s.name).Err
}

func (s *Shell) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// SetTray shows or hides the icon and sets its tooltip. A hidden icon is "Passive", which trays
// leave out. The menu is rebuilt too, so its disabled items follow the queue.
func (s *Shell) SetTray(visible bool, tooltip string) {
	s.mu.Lock()
	changedStatus := visible != s.visible
	changedTip := tooltip != s.tooltip
	s.visible, s.tooltip = visible, tooltip
	s.mu.Unlock()

	if changedStatus {
		status := "Passive"
		if visible {
			status = "Active"
		}
		s.props.SetMust(itemIface, "Status", status)
		_ = s.conn.Emit(itemPath, itemIface+".NewStatus", status)
	}
	if changedTip {
		s.props.SetMust(itemIface, "ToolTip", splitToolTip(tooltip))
		_ = s.conn.Emit(itemPath, itemIface+".NewToolTip")
	}
	s.rebuildMenu()
}

// splitToolTip makes the tooltip's first line its title and the rest its text.
func splitToolTip(tooltip string) toolTip {
	title, body, _ := cutLine(tooltip)
	return toolTip{Title: title, Body: body, Icons: []pixmap{}}
}

func cutLine(s string) (first, rest string, found bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// SetProgress does nothing: Linux has no standard taskbar progress.
func (s *Shell) SetProgress(ProgressState, float64) {}

// Refresh rebuilds the menu.
func (s *Shell) Refresh() { s.rebuildMenu() }

// Close takes the icon off the bus.
func (s *Shell) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.conn.Close()
}

// rebuildMenu asks for the menu again and tells the tray when it changed.
func (s *Shell) rebuildMenu() bool {
	if s.opts.Menu == nil {
		return false
	}
	items := s.opts.Menu()
	s.mu.Lock()
	if slices.Equal(items, s.items) {
		s.mu.Unlock()
		return false
	}
	s.items = items
	s.revision++
	rev := s.revision
	s.mu.Unlock()
	_ = s.conn.Emit(menuPath, menuIface+".LayoutUpdated", rev, int32(0))
	return true
}

// ---- the StatusNotifierItem's methods ----

type sniItem Shell

func (i *sniItem) Activate(x, y int32) *dbus.Error {
	if i.opts.OnOpen != nil {
		go i.opts.OnOpen()
	}
	return nil
}

func (i *sniItem) SecondaryActivate(x, y int32) *dbus.Error { return i.Activate(x, y) }

// ContextMenu is for trays that don't draw the dbusmenu themselves; ours is always there.
func (i *sniItem) ContextMenu(x, y int32) *dbus.Error { return nil }

func (i *sniItem) Scroll(delta int32, orientation string) *dbus.Error { return nil }

// ---- the dbusmenu ----

type sniMenu Shell

// menuLayout is a dbusmenu node: (ia{sv}av), its children being layouts too.
type menuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

type menuItemProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

// nodes is the menu as dbusmenu items, root (0) first.
func (m *sniMenu) nodes() (uint32, []menuItemProps) {
	m.mu.Lock()
	items, rev := m.items, m.revision
	m.mu.Unlock()
	nodes := []menuItemProps{{ID: 0, Props: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}}}
	for i, it := range items {
		if it.ID == 0 {
			nodes = append(nodes, menuItemProps{ID: int32(separatorIDs + i), Props: map[string]dbus.Variant{
				"type": dbus.MakeVariant("separator"),
			}})
			continue
		}
		nodes = append(nodes, menuItemProps{ID: int32(it.ID), Props: map[string]dbus.Variant{
			"label":   dbus.MakeVariant(it.Label),
			"enabled": dbus.MakeVariant(!it.Disabled),
			"visible": dbus.MakeVariant(true),
		}})
	}
	return rev, nodes
}

func (m *sniMenu) GetLayout(parentID, recursionDepth int32, propertyNames []string) (uint32, menuLayout, *dbus.Error) {
	rev, nodes := m.nodes()
	root := menuLayout{ID: 0, Props: nodes[0].Props, Children: []dbus.Variant{}}
	if parentID != 0 {
		for _, n := range nodes[1:] {
			if n.ID == parentID {
				return rev, menuLayout{ID: n.ID, Props: n.Props, Children: []dbus.Variant{}}, nil
			}
		}
		return rev, root, dbus.MakeFailedError(fmt.Errorf("no menu item %d", parentID))
	}
	if recursionDepth != 0 {
		for _, n := range nodes[1:] {
			root.Children = append(root.Children, dbus.MakeVariant(menuLayout{ID: n.ID, Props: n.Props, Children: []dbus.Variant{}}))
		}
	}
	return rev, root, nil
}

func (m *sniMenu) GetGroupProperties(ids []int32, propertyNames []string) ([]menuItemProps, *dbus.Error) {
	_, nodes := m.nodes()
	out := []menuItemProps{}
	for _, n := range nodes {
		if len(ids) == 0 || slices.Contains(ids, n.ID) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *sniMenu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	_, nodes := m.nodes()
	for _, n := range nodes {
		if n.ID == id {
			if v, ok := n.Props[name]; ok {
				return v, nil
			}
		}
	}
	return dbus.MakeVariant(""), dbus.MakeFailedError(fmt.Errorf("no property %s on item %d", name, id))
}

func (m *sniMenu) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventID != "clicked" || id <= 0 || id >= separatorIDs || m.opts.OnMenu == nil {
		return nil
	}
	m.mu.Lock()
	known := slices.ContainsFunc(m.items, func(it MenuItem) bool { return it.ID == int(id) && !it.Disabled })
	m.mu.Unlock()
	if known {
		go m.opts.OnMenu(int(id))
	}
	return nil
}

func (m *sniMenu) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, e := range events {
		_ = m.Event(e.ID, e.EventID, e.Data, e.Timestamp)
	}
	return []int32{}, nil
}

// AboutToShow is the menu opening: build it again, like the Windows menu is on every open.
func (m *sniMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	return (*Shell)(m).rebuildMenu(), nil
}

func (m *sniMenu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	if (*Shell)(m).rebuildMenu() {
		return []int32{0}, []int32{}, nil
	}
	return []int32{}, []int32{}, nil
}

// ---- icon and tooltip ----

// pixmap is one size of an icon: width, height and ARGB32 pixels in network byte order.
type pixmap struct {
	W, H int32
	Data []byte
}

type toolTip struct {
	IconName string
	Icons    []pixmap
	Title    string
	Body     string
}

// iconSizes are the sizes the icon is offered in; the tray picks the nearest.
var iconSizes = []int{16, 22, 24, 32, 48, 64, 128}

// iconPixmaps scales a PNG to iconSizes. A missing or broken icon gives none, and the tray
// shows its generic one.
func iconPixmaps(png []byte) []pixmap {
	out := []pixmap{}
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return out
	}
	for _, size := range iconSizes {
		out = append(out, pixmap{W: int32(size), H: int32(size), Data: scaleARGB(img, size)})
	}
	return out
}

// scaleARGB shrinks img to size×size by averaging the source pixels under each one, weighted by
// their alpha so transparent edges don't darken.
func scaleARGB(img image.Image, size int) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, 0, size*size*4)
	for y := 0; y < size; y++ {
		y0, y1 := b.Min.Y+y*h/size, b.Min.Y+max((y+1)*h/size, y*h/size+1)
		for x := 0; x < size; x++ {
			x0, x1 := b.Min.X+x*w/size, b.Min.X+max((x+1)*w/size, x*w/size+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBA64Model.Convert(img.At(sx, sy)).(color.NRGBA64)
					ca := uint64(c.A)
					r += uint64(c.R) * ca
					g += uint64(c.G) * ca
					bl += uint64(c.B) * ca
					a += ca
					n++
				}
			}
			var px [4]byte
			if a > 0 {
				px = [4]byte{byte(a / n >> 8), byte(r / a >> 8), byte(g / a >> 8), byte(bl / a >> 8)}
			}
			out = append(out, px[:]...)
		}
	}
	return out
}

// ---- whether the window is in front ----

// The page reports focus changes (SetMainWindowFocused): GTK on Wayland can't be asked which
// window has the focus. It starts focused, the way the window opens.
var mainFocused atomic.Bool

func init() { mainFocused.Store(true) }

// SetMainWindowFocused records whether the main window has the keyboard focus.
func SetMainWindowFocused(focused bool) { mainFocused.Store(focused) }

// MainWindowInFront reports whether the main window is the one the user is working in. The app
// checks separately whether it is hidden.
func MainWindowInFront() bool { return mainFocused.Load() }
