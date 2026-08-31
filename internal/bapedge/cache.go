package bapedge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type GrantCache struct {
	directory  string
	maxAge     time.Duration
	maxEntries int
	maxBytes   int64
}

const (
	grantCacheMaxAge     = time.Hour
	grantCacheMaxEntries = 1024
	grantCacheMaxBytes   = 16 * 1024 * 1024
)

type GrantCacheStats struct {
	Entries int
	Bytes   int64
	Oldest  time.Duration
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
	cache := &GrantCache{directory: directory, maxAge: grantCacheMaxAge, maxEntries: grantCacheMaxEntries, maxBytes: grantCacheMaxBytes}
	if _, err := cache.Prune(time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("prune signed grant cache: %w", err)
	}
	return cache, nil
}

func (c *GrantCache) Load(requestHash string) (string, error) {
	if err := validateCacheKey(requestHash); err != nil {
		return "", err
	}
	path := c.path(requestHash)
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if c.maxAge > 0 && time.Since(info.ModTime()) > c.maxAge {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *GrantCache) Store(requestHash, grant string) error {
	if err := validateCacheKey(requestHash); err != nil {
		return err
	}
	if c.maxBytes > 0 && int64(len(grant)) > c.maxBytes {
		return fmt.Errorf("signed grant exceeds cache byte limit")
	}
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
		_, err = c.Prune(time.Now().UTC())
		return err
	}
	// Windows does not replace an existing file with Rename. The cache is
	// non-authoritative; delete only this exact hash entry and replace it.
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	_, err = c.Prune(time.Now().UTC())
	return err
}

func (c *GrantCache) path(requestHash string) string {
	return filepath.Join(c.directory, requestHash+".grant")
}

func (c *GrantCache) Prune(now time.Time) (GrantCacheStats, error) {
	type entry struct {
		path    string
		modTime time.Time
		size    int64
	}
	items := make([]entry, 0)
	directoryEntries, err := os.ReadDir(c.directory)
	if err != nil {
		return GrantCacheStats{}, err
	}
	for _, directoryEntry := range directoryEntries {
		if directoryEntry.IsDir() || filepath.Ext(directoryEntry.Name()) != ".grant" {
			continue
		}
		info, err := directoryEntry.Info()
		if err != nil {
			return GrantCacheStats{}, err
		}
		path := filepath.Join(c.directory, directoryEntry.Name())
		if c.maxAge > 0 && now.Sub(info.ModTime()) > c.maxAge {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return GrantCacheStats{}, err
			}
			continue
		}
		items = append(items, entry{path: path, modTime: info.ModTime(), size: info.Size()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].modTime.Before(items[j].modTime) })
	var totalBytes int64
	for _, item := range items {
		totalBytes += item.size
	}
	for len(items) > 0 && ((c.maxEntries > 0 && len(items) > c.maxEntries) || (c.maxBytes > 0 && totalBytes > c.maxBytes)) {
		oldest := items[0]
		if err := os.Remove(oldest.path); err != nil && !os.IsNotExist(err) {
			return GrantCacheStats{}, err
		}
		totalBytes -= oldest.size
		items = items[1:]
	}
	stats := GrantCacheStats{Entries: len(items), Bytes: totalBytes}
	if len(items) > 0 {
		stats.Oldest = now.Sub(items[0].modTime)
		if stats.Oldest < 0 {
			stats.Oldest = 0
		}
	}
	return stats, nil
}

func validateCacheKey(value string) error {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, `/\\`) || value == "." || value == ".." {
		return fmt.Errorf("invalid signed grant cache key")
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return fmt.Errorf("invalid signed grant cache key")
		}
	}
	return nil
}
