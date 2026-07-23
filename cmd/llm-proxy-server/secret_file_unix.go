//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"io"
	"os"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

const maxDeploymentSecretFileBytes = 4096

func readDeploymentSecretFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if err == unix.ELOOP {
			return nil, errors.New("secret file must be a regular file and not a symlink")
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("secret file must be a regular file and not a symlink")
	}
	if info.Size() > maxDeploymentSecretFileBytes {
		return nil, errors.New("secret file is too large")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxDeploymentSecretFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxDeploymentSecretFileBytes {
		return nil, errors.New("secret file is too large")
	}
	return contents, nil
}
