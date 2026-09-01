//go:build !windows

package index

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type processLock struct {
	file *os.File
}

func acquireProcessLock(path string) (*processLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another qqmailctl process holds the cache lock: %w", err)
	}
	return &processLock{file: file}, nil
}

func (l *processLock) close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
