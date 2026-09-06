package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var inProcessWriteLocks sync.Map

// WithWriteLock runs fn while holding the store's inter-process write lock.
func (s *FileStore) WithWriteLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.paths.LockPath), PrivateDirPerm); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}

	lockKey, err := canonicalWriteLockKey(s.paths.LockPath)
	if err != nil {
		return fmt.Errorf("resolve write lock path: %w", err)
	}
	localLockValue, _ := inProcessWriteLocks.LoadOrStore(lockKey, &sync.Mutex{})
	localLock := localLockValue.(*sync.Mutex)
	localLock.Lock()
	defer localLock.Unlock()

	file, err := os.OpenFile(s.paths.LockPath, os.O_CREATE|os.O_RDWR, SecretFilePerm)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer file.Close()

	if err := fcntlLock(file, syscall.F_WRLCK); err != nil {
		return fmt.Errorf("acquire write lock: %w", err)
	}
	defer func() {
		_ = fcntlLock(file, syscall.F_UNLCK)
	}()

	return fn()
}

func canonicalWriteLockKey(path string) (string, error) {
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}

	canonicalDir, err := filepath.EvalSymlinks(filepath.Dir(absolutePath))
	if err != nil {
		return "", err
	}
	return filepath.Join(canonicalDir, filepath.Base(absolutePath)), nil
}

func fcntlLock(file *os.File, lockType int16) error {
	lock := syscall.Flock_t{
		Type:   lockType,
		Whence: 0,
		Start:  0,
		Len:    0,
	}

	for {
		if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLKW, &lock); err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return err
		}
		return nil
	}
}
