package deviceclient

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

func DefaultCachePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", errors.Wrap(err, "resolve user config directory")
	}
	return filepath.Join(base, "llm-proxy", "agent-credential.json"), nil
}

func LoadCredential(path string) (Credential, error) {
	var credential Credential
	err := withLockedCache(path, func() error {
		data, err := readPrivateFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return ErrCacheNotFound
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &credential); err != nil {
			return errors.Wrap(err, "decode agent credential cache")
		}
		if credential.Token == "" || credential.ClientInstanceID == "" {
			return errors.New("agent credential cache is incomplete")
		}
		return nil
	})
	return credential, err
}

func SaveCredential(path string, credential Credential) error {
	if credential.Token == "" || credential.ClientInstanceID == "" {
		return errors.New("agent credential and client instance ID are required")
	}
	return withLockedCache(path, func() error {
		data, err := json.MarshalIndent(credential, "", "  ")
		if err != nil {
			return errors.Wrap(err, "encode agent credential cache")
		}
		return atomicWritePrivate(path, append(data, '\n'))
	})
}

func DeleteCredential(path string) error {
	return withLockedCache(path, func() error {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.Wrap(err, "delete agent credential cache")
		}
		return nil
	})
}

func LoadOrCreateClientInstanceID(path string) (string, error) {
	var id string
	err := withLockedCache(path, func() error {
		data, err := readPrivateFile(path)
		if err == nil {
			var credential Credential
			if json.Unmarshal(data, &credential) == nil && credential.ClientInstanceID != "" {
				id = credential.ClientInstanceID
				return nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return errors.Wrap(err, "generate client instance ID")
		}
		id = hex.EncodeToString(random[:])
		data, err = json.MarshalIndent(Credential{ClientInstanceID: id}, "", "  ")
		if err != nil {
			return errors.Wrap(err, "encode client instance state")
		}
		return atomicWritePrivate(path, append(data, '\n'))
	})
	return id, err
}

var ErrCacheNotFound = errors.New("agent credential cache not found")
