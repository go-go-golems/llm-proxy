//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package main

import "github.com/pkg/errors"

func readDeploymentSecretFile(string) ([]byte, error) {
	return nil, errors.New("deployment secret-file input is supported only on POSIX platforms")
}
