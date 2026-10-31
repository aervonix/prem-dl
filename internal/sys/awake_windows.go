//go:build windows

package sys

import (
	"errors"
	"runtime"
)

var setThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

// holdAwake asks Windows not to sleep. The request belongs to the thread that made it and ends
// with it, so a thread is kept for it until it is released.
func holdAwake() (func(), error) {
	done := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if r, _, _ := setThreadExecutionState.Call(esContinuous | esSystemRequired); r == 0 {
			result <- errors.New("SetThreadExecutionState failed")
			return
		}
		result <- nil
		<-done
		setThreadExecutionState.Call(esContinuous)
	}()
	if err := <-result; err != nil {
		return nil, err
	}
	return func() { close(done) }, nil
}
