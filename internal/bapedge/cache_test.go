package bapedge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGrantCacheUsesExactRequestHash(t *testing.T) {
	cache, err := NewGrantCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Store("request-one", "signed-grant"); err != nil {
		t.Fatal(err)
	}
	value, err := cache.Load("request-one")
	if err != nil || value != "signed-grant" {
		t.Fatalf("cached grant = %q, err = %v", value, err)
	}
	value, err = cache.Load("request-two")
	if err != nil || value != "" {
		t.Fatalf("different request unexpectedly hit cache: %q, err = %v", value, err)
	}
	if filepath.Dir(cache.path("request-one")) != cache.directory {
		t.Fatal("cache entry escaped cache directory")
	}
}

func TestGrantCacheRejectsPathTraversal(t *testing.T) {
	cache, err := NewGrantCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.Store(`..\outside`, "signed-grant"); err == nil {
		t.Fatal("cache accepted a path-traversal key")
	}
}

func TestGrantCachePrunesExpiredAndOldestEntries(t *testing.T) {
	cache, err := NewGrantCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cache.maxAge = time.Hour
	cache.maxEntries = 2
	cache.maxBytes = 1024
	for _, key := range []string{"one", "two", "three"} {
		if err := cache.Store(key, key); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if value, err := cache.Load("one"); err != nil || value != "" {
		t.Fatalf("oldest cache entry was not evicted: value=%q err=%v", value, err)
	}
	expiredPath := cache.path("two")
	expired := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(expiredPath, expired, expired); err != nil {
		t.Fatal(err)
	}
	stats, err := cache.Prune(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entries != 1 {
		t.Fatalf("cache entries after expiry = %d, want 1", stats.Entries)
	}
}
