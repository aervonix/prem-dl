package settings

import "golang.org/x/sys/windows"

// downloadsFolder asks Windows for FOLDERID_Downloads, wherever the user moved it.
func downloadsFolder() string {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return ""
	}
	return dir
}
