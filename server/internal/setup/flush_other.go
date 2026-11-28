//go:build !linux

package setup

import "errors"

// flushInput cannot discard pending terminal input outside Linux, where setup does not run
// (scripts/setup runs it in a Linux container). The owner question then writes no owner.
func flushInput(int) error {
	return errors.New("discarding pending terminal input is supported on Linux only")
}
