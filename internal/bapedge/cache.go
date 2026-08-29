package bapedge

import (
	"fmt"
	"os"
	"path/filepath"
)

type GrantCache struct {
	directory string
}

func NewGrantCache(configuredDirectory string) (*GrantCache, error) {
	directory := configuredDirectory
	if directory == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		directory = filepath.Join(base, "BAP Edge", "grants")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, fmt.Errorf("create signed grant cache: %w", err)
	}
	return &GrantCache{directory: directory}, nil
}

func (c *GrantCache) Load(requestHash string) (string, error) {
	data, err := os.ReadFile(c.path(requestHash))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *GrantCache) Store(requestHash, grant string) error {
	temporary, err := os.CreateTemp(c.directory, "grant-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(grant); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	destination := c.path(requestHash)
	if err := os.Rename(temporaryPath, destination); err == nil {
		return nil
	}
	// Windows does not replace an existing file with Rename. The cache is
	// non-authoritative; delete only this exact hash entry and replace it.
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func (c *GrantCache) path(requestHash string) string {
	return filepath.Join(c.directory, requestHash+".grant")
}
