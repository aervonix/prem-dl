//go:build !windows && !linux

package settings

func downloadsFolder() string { return "" }
