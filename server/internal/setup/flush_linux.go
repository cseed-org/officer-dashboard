package setup

import "golang.org/x/sys/unix"

// flushInput discards input that is waiting on the terminal fd and has not been read
// (tcflush(fd, TCIFLUSH)), such as a second line pasted together with the setup code.
// The setup container is Linux.
func flushInput(fd int) error {
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}
