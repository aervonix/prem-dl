//go:build !windows && !linux

package sys

import (
	"errors"
	"os"
	"strings"
)

func AppPath() (string, error)                      { return os.Executable() }
func CommandLine(exe string, args ...string) string { return exe + " " + strings.Join(args, " ") }
func SchemeCommand(exe string) string               { return exe }

func RegisterURLScheme(scheme, name, exe string) error { return nil }
func URLSchemeCommand(string) string                   { return "" }
func SetRunAtSignIn(name, command string) error        { return errors.New("not supported") }
func RunAtSignIn(string) string                        { return "" }
func SetToastName(id, name string) error               { return nil }
func InstalledDir(name, exe string) string             { return "" }
func StartCommandLine(exe, cmdline string) error       { return errors.New("not supported") }
