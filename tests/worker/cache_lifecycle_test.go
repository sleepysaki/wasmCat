package worker_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
	"wasmcat/internal/worker"
)

func TestModuleCacheUsesDigestAwareKeys(t *testing.T) {
	ctx := context.Background()

	var downloads int
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "sha256:one", ""); err != nil {
		t.Fatalf("first fetch returned error: %v", err)
	}
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "sha256:one", ""); err != nil {
		t.Fatalf("same digest fetch returned error: %v", err)
	}
	if downloads != 1 {
		t.Fatalf("expected same digest to download once, got %d downloads", downloads)
	}

	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "sha256:two", ""); err != nil {
		t.Fatalf("changed digest fetch returned error: %v", err)
	}
	if downloads != 2 {
		t.Fatalf("expected changed digest to download again, got %d downloads", downloads)
	}
	if stats := engine.CacheStats(); stats.Entries != 2 {
		t.Fatalf("expected two digest cache entries, got %+v", stats)
	}
}

func TestModuleCacheTTLCausesRefetch(t *testing.T) {
	ctx := context.Background()

	var downloads int
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{ModuleCacheTTL: 20 * time.Millisecond})
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "sha256:ttl", ""); err != nil {
		t.Fatalf("first fetch returned error: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "sha256:ttl", ""); err != nil {
		t.Fatalf("fetch after ttl returned error: %v", err)
	}

	if downloads != 2 {
		t.Fatalf("expected ttl expiration to refetch module, got %d downloads", downloads)
	}
}

func TestModuleCacheEvictsLeastRecentlyUsedByEntryCount(t *testing.T) {
	ctx := context.Background()

	downloads := map[string]int{}
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads[r.URL.Path]++
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		MaxCachedModules: 2,
		MaxCacheBytes:    1 << 20,
	})

	mustFetchDigest(t, engine, moduleServer.URL+"/a", "a")
	mustFetchDigest(t, engine, moduleServer.URL+"/b", "b")
	mustFetchDigest(t, engine, moduleServer.URL+"/a", "a")
	mustFetchDigest(t, engine, moduleServer.URL+"/c", "c")
	mustFetchDigest(t, engine, moduleServer.URL+"/b", "b")

	if downloads["/b"] != 2 {
		t.Fatalf("expected least recently used module b to be refetched, got %d downloads", downloads["/b"])
	}
	if stats := engine.CacheStats(); stats.Entries != 2 {
		t.Fatalf("expected cache to stay at two entries, got %+v", stats)
	}
}

func TestModuleCacheEvictsByByteBudget(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		MaxCachedModules: 10,
		MaxCacheBytes:    int64(len(wasmEchoModule())) + 1,
	})

	mustFetchDigest(t, engine, moduleServer.URL+"/a", "a")
	mustFetchDigest(t, engine, moduleServer.URL+"/b", "b")

	stats := engine.CacheStats()
	if stats.Entries != 1 {
		t.Fatalf("expected byte budget to evict down to one entry, got %+v", stats)
	}
	if stats.Bytes > stats.MaxBytes {
		t.Fatalf("expected cache bytes <= max bytes, got %+v", stats)
	}
}

func TestModuleCacheCoalescesConcurrentColdFetches(t *testing.T) {
	ctx := context.Background()

	var mu sync.Mutex
	downloads := 0
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		downloads++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{MaxConcurrentExecs: 8})

	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "sha256:cold", "")
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent fetch returned error: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if downloads != 1 {
		t.Fatalf("expected one cold download, got %d", downloads)
	}
}

func mustFetchDigest(t *testing.T, engine *worker.WasmEngine, moduleURL string, digest string) {
	t.Helper()

	if err := engine.FetchAndCacheWithDigest(context.Background(), "echo", moduleURL, "sha256:"+digest, ""); err != nil {
		t.Fatalf("fetch %s returned error: %v", digest, err)
	}
}
