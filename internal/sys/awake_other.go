//go:build !windows && !linux

package sys

import "errors"

func holdAwake() (func(), error) { return nil, errors.New("not supported here") }
