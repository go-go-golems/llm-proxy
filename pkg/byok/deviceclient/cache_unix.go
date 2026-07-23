//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package deviceclient

import (
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func withLockedCache(path string, operation func() error) error {
	if path == "" {
		return errors.New("agent credential cache path is required")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errors.Wrap(err, "create agent cache directory")
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return errors.Wrap(err, "inspect agent cache directory")
	}
	if !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o700 {
		return errors.New("agent cache directory must be a mode-0700 directory and not a symlink")
	}
	lockPath := path + ".lock"
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.Wrap(err, "open agent cache lock")
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer func() { _ = lock.Close() }()
	lockInfo, err := lock.Stat()
	if err != nil {
		return errors.Wrap(err, "inspect agent cache lock")
	}
	if !lockInfo.Mode().IsRegular() || lockInfo.Mode().Perm() != 0o600 {
		return errors.New("agent cache lock must be a mode-0600 regular file")
	}
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		return errors.Wrap(err, "lock agent credential cache")
	}
	defer func() { _ = unix.Flock(fd, unix.LOCK_UN) }()
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return errors.New("agent credential cache must be a mode-0600 regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.Wrap(err, "inspect agent credential cache")
	}
	return operation()
}

func readPrivateFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.Wrap(err, "open agent credential cache")
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Wrap(err, "inspect agent credential cache")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return nil, errors.New("agent credential cache must be a mode-0600 regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1<<20))
	if err != nil {
		return nil, errors.Wrap(err, "read agent credential cache")
	}
	return data, nil
}

func atomicWritePrivate(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".agent-credential-*")
	if err != nil {
		return errors.Wrap(err, "create temporary agent credential cache")
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return errors.Wrap(err, "secure temporary agent credential cache")
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return errors.Wrap(err, "write temporary agent credential cache")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.Wrap(err, "sync temporary agent credential cache")
	}
	if err := temporary.Close(); err != nil {
		return errors.Wrap(err, "close temporary agent credential cache")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.Wrap(err, "replace agent credential cache")
	}
	directoryFD, err := unix.Open(directory, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.Wrap(err, "open agent cache directory")
	}
	defer func() { _ = unix.Close(directoryFD) }()
	return errors.Wrap(unix.Fsync(directoryFD), "sync agent cache directory")
}
