//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package deviceclient

import "github.com/pkg/errors"

func withLockedCache(string, func() error) error {
	return errors.New("agent credential cache is supported only on POSIX platforms")
}

func readPrivateFile(string) ([]byte, error) {
	return nil, errors.New("agent credential cache is supported only on POSIX platforms")
}

func atomicWritePrivate(string, []byte) error {
	return errors.New("agent credential cache is supported only on POSIX platforms")
}
