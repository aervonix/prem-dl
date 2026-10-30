//go:build !windows

package sys

import "errors"

func RegisterURLScheme(scheme, name, exe string) error { return nil }
func URLSchemeCommand(string) string                   { return "" }
func SetRunAtSignIn(name, command string) error        { return errors.New("not supported") }
func RunAtSignIn(string) string                        { return "" }
func SetToastName(id, name string) error               { return nil }
func InstalledDir(name, exe string) string             { return "" }
func StartCommandLine(exe, cmdline string) error       { return errors.New("not supported") }
