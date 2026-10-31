//go:build !linux

package main

import "errors"

// Only Linux copies run from an AppImage.
func appImagePath() string { return "" }

func swapAppImage(string, string) error { return errors.New("not supported") }
