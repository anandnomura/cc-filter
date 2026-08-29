package bapedge

import (
	"path/filepath"
	"testing"
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
