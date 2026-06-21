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

	moduleOne := wasmEchoModule()
	moduleTwo := wasmEchoModuleWithCustomSection("two")
	var downloads int
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		if downloads == 1 {
			w.Write(moduleOne)
			return
		}
		w.Write(moduleTwo)
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, sha256Digest(moduleOne), ""); err != nil {
		t.Fatalf("first fetch returned error: %v", err)
	}
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, sha256Digest(moduleOne), ""); err != nil {
		t.Fatalf("same digest fetch returned error: %v", err)
	}
	if downloads != 1 {
		t.Fatalf("expected same digest to download once, got %d downloads", downloads)
	}

	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, sha256Digest(moduleTwo), ""); err != nil {
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

	moduleBytes := wasmEchoModule()
	var downloads int
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.Write(moduleBytes)
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{ModuleCacheTTL: 20 * time.Millisecond})
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, sha256Digest(moduleBytes), ""); err != nil {
		t.Fatalf("first fetch returned error: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, sha256Digest(moduleBytes), ""); err != nil {
		t.Fatalf("fetch after ttl returned error: %v", err)
	}

	if downloads != 2 {
		t.Fatalf("expected ttl expiration to refetch module, got %d downloads", downloads)
	}
}

func TestModuleCacheEvictsLeastRecentlyUsedByEntryCount(t *testing.T) {
	ctx := context.Background()

	modules := map[string][]byte{
		"/a": wasmEchoModuleWithCustomSection("a"),
		"/b": wasmEchoModuleWithCustomSection("b"),
		"/c": wasmEchoModuleWithCustomSection("c"),
	}
	downloads := map[string]int{}
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads[r.URL.Path]++
		w.Write(modules[r.URL.Path])
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		MaxCachedModules: 2,
		MaxCacheBytes:    1 << 20,
	})

	mustFetchDigest(t, engine, moduleServer.URL+"/a", modules["/a"])
	mustFetchDigest(t, engine, moduleServer.URL+"/b", modules["/b"])
	mustFetchDigest(t, engine, moduleServer.URL+"/a", modules["/a"])
	mustFetchDigest(t, engine, moduleServer.URL+"/c", modules["/c"])
	mustFetchDigest(t, engine, moduleServer.URL+"/b", modules["/b"])

	if downloads["/b"] != 2 {
		t.Fatalf("expected least recently used module b to be refetched, got %d downloads", downloads["/b"])
	}
	if stats := engine.CacheStats(); stats.Entries != 2 {
		t.Fatalf("expected cache to stay at two entries, got %+v", stats)
	}
}

func TestModuleCacheEvictsByByteBudget(t *testing.T) {
	ctx := context.Background()

	modules := map[string][]byte{
		"/a": wasmEchoModuleWithCustomSection("a"),
		"/b": wasmEchoModuleWithCustomSection("b"),
	}
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(modules[r.URL.Path])
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{
		MaxCachedModules: 10,
		MaxCacheBytes:    int64(len(modules["/a"])) + 1,
	})

	mustFetchDigest(t, engine, moduleServer.URL+"/a", modules["/a"])
	mustFetchDigest(t, engine, moduleServer.URL+"/b", modules["/b"])

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

	moduleBytes := wasmEchoModule()
	var mu sync.Mutex
	downloads := 0
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		downloads++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		w.Write(moduleBytes)
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngineWithLimits(ctx, worker.Limits{MaxConcurrentExecs: 8})

	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, sha256Digest(moduleBytes), "")
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

func mustFetchDigest(t *testing.T, engine *worker.WasmEngine, moduleURL string, moduleBytes []byte) {
	t.Helper()

	digest := sha256Digest(moduleBytes)
	if err := engine.FetchAndCacheWithDigest(context.Background(), "echo", moduleURL, digest, ""); err != nil {
		t.Fatalf("fetch %s returned error: %v", digest, err)
	}
}

func wasmEchoModuleWithCustomSection(name string) []byte {
	module := append([]byte{}, wasmEchoModule()...)
	nameBytes := []byte(name)
	payloadLen := len(nameBytes) + 2
	module = append(module, 0x00, byte(payloadLen), byte(len(nameBytes)))
	module = append(module, nameBytes...)
	module = append(module, 0x00)
	return module
}
