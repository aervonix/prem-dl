package sys

// ProgressState is what the taskbar button's progress bar shows.
type ProgressState int

const (
	ProgressNone ProgressState = iota
	ProgressIndeterminate
	ProgressNormal
	ProgressPaused // yellow
	ProgressError  // red
)

// MenuItem is one line of the tray icon's menu. A zero ID makes a separator.
type MenuItem struct {
	ID       int
	Label    string
	Default  bool // shown in bold where the tray can: what clicking the icon does
	Disabled bool
}

// ShellOptions are the tray icon's callbacks. They run on a goroutine of their own, except Menu,
// which runs while the menu is being built and must return quickly.
type ShellOptions struct {
	OnOpen func()            // the icon was clicked, or selected with the keyboard
	Menu   func() []MenuItem // the menu, built each time it opens
	OnMenu func(id int)      // a menu item was chosen
	Icon   []byte            // the icon as a PNG, where the tray needs one (Windows uses the exe's)
}
