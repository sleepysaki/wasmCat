package worker

import (
	"context" // manage lifecycle, kill fnc after timeout to save resources
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"wasmcat/internal/shared"

	"github.com/tetratelabs/wazero"
)

type ModuleCacheKey struct {
	ModuleName string
	ModuleURL  string
	Digest     string
}

func (k ModuleCacheKey) String() string {
	if k.Digest != "" {
		return k.ModuleName + "@digest:" + k.Digest
	}
	if k.ModuleURL != "" {
		return k.ModuleName + "@url:" + k.ModuleURL
	}

	return k.ModuleName
}

type ModuleCacheStats struct {
	Entries    int
	Bytes      int64
	MaxEntries int
	MaxBytes   int64
}

type moduleCacheEntry struct {
	key        ModuleCacheKey
	compiled   wazero.CompiledModule
	sizeBytes  int64
	createdAt  time.Time
	lastUsedAt time.Time
	usedOrder  uint64
}

type WasmEngine struct {
	// Store wazero.Runtime aka engine and image caches to avoid reloading and recompiling for each execution
	runtime wazero.Runtime
	cache   map[string]moduleCacheEntry
	// inflight marks cache keys currently being downloaded and compiled.
	// Followers wait for the leader instead of repeating the same expensive cold compile.
	inflight   map[string]chan struct{}
	cacheBytes int64
	cacheClock uint64
	client     *http.Client
	mu         sync.RWMutex
	limits     Limits
	sem        chan struct{}
}

func NewWasmEngine(ctx context.Context) *WasmEngine {
	return NewWasmEngineWithLimits(ctx, DefaultLimits)
}

func NewWasmEngineWithLimits(ctx context.Context, limits Limits) *WasmEngine {
	limits = normalizeLimits(limits)

	return &WasmEngine{
		runtime:  wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true)),
		cache:    make(map[string]moduleCacheEntry),
		inflight: make(map[string]chan struct{}),
		client:   shared.NewHTTPClient(),
		limits:   limits,
		sem:      make(chan struct{}, limits.MaxConcurrentExecs),
	}
}

func (e *WasmEngine) Limits() Limits {
	return e.limits
}

func (e *WasmEngine) CacheStats() shared.ModuleCacheStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return shared.ModuleCacheStats{
		Entries:    len(e.cache),
		Bytes:      e.cacheBytes,
		MaxEntries: e.limits.MaxCachedModules,
		MaxBytes:   e.limits.MaxCacheBytes,
	}
}

func (e *WasmEngine) ClearCache(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for key, entry := range e.cache {
		_ = entry.compiled.Close(ctx)
		delete(e.cache, key)
	}
	e.cacheBytes = 0
}

func (e *WasmEngine) FetchAndCache(ctx context.Context, moduleName, moduleURL string, bearerToken string) error {
	return e.FetchAndCacheWithDigest(ctx, moduleName, moduleURL, "", bearerToken)
}

func (e *WasmEngine) FetchAndCacheWithDigest(ctx context.Context, moduleName, moduleURL string, moduleDigest string, bearerToken string) error {
	cacheKey := ModuleCacheKey{
		ModuleName: strings.TrimSpace(moduleName),
		ModuleURL:  strings.TrimSpace(moduleURL),
		Digest:     strings.TrimSpace(moduleDigest),
	}
	cacheKeyString := cacheKey.String()

	// First check the cache. A hit also updates lastUsedAt so eviction can keep hot modules.
	if e.touchCachedModule(cacheKeyString, time.Now()) {
		slog.Info("module cache hit", "module_name", moduleName, "module_digest", moduleDigest)
		return nil
	}

	// The worker needs a URL when the module is not in cache yet.
	// Later, the master can point this at ACR, blob storage, or any module store.
	if moduleURL == "" {
		return fmt.Errorf("module %s has no module URL", moduleName)
	}

	for {
		compileDone, leader := e.beginCompile(cacheKeyString)
		if leader {
			defer e.finishCompile(cacheKeyString)
			break
		}

		select {
		case <-compileDone:
			if e.touchCachedModule(cacheKeyString, time.Now()) {
				slog.Info("module cache hit after wait", "module_name", moduleName, "module_digest", moduleDigest)
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Another goroutine may have filled the cache just before this goroutine became the leader.
	if e.touchCachedModule(cacheKeyString, time.Now()) {
		slog.Info("module cache hit", "module_name", moduleName, "module_digest", moduleDigest)
		return nil
	}

	compiled, wasmBytes, err := e.downloadAndCompile(ctx, moduleName, moduleURL, moduleDigest, bearerToken)
	if err != nil {
		return err
	}

	// Take the write lock only when changing the map.
	// The cache entry stores lifecycle data so later requests can enforce TTL and eviction limits.
	e.mu.Lock()
	now := time.Now()
	if _, exists := e.cache[cacheKeyString]; !exists {
		e.cacheClock++
		e.cache[cacheKeyString] = moduleCacheEntry{
			key:        cacheKey,
			compiled:   compiled,
			sizeBytes:  int64(len(wasmBytes)),
			createdAt:  now,
			lastUsedAt: now,
			usedOrder:  e.cacheClock,
		}
		e.cacheBytes += int64(len(wasmBytes))
		e.evictLocked(ctx, now)
		slog.Info("module compiled and cached", "module_name", moduleName, "module_url", moduleURL, "module_digest", moduleDigest, "module_bytes", len(wasmBytes), "cache_entries", len(e.cache), "cache_bytes", e.cacheBytes)
	} else {
		_ = compiled.Close(ctx)
	}
	e.mu.Unlock()

	return nil
}

func (e *WasmEngine) Execute(ctx context.Context, moduleName string, moduleURL string, payload string, bearerToken string) (result string, err error) {
	return e.ExecuteWithDigest(ctx, moduleName, moduleURL, "", payload, bearerToken)
}

func (e *WasmEngine) ExecuteWithDigest(ctx context.Context, moduleName string, moduleURL string, moduleDigest string, payload string, bearerToken string) (result string, err error) {
	start := time.Now()
	slog.Info("wasm execution started", "module_name", moduleName, "module_digest", moduleDigest, "payload_bytes", len(payload))
	defer func() {
		if err != nil {
			slog.Error("wasm execution failed", "module_name", moduleName, "duration_ms", time.Since(start).Milliseconds(), "error", err)
		}
	}()

	// Reject big payloads before downloading or running anything.
	// This protects both Go memory and the Wasm module's linear memory.
	if int64(len(payload)) > e.limits.MaxPayloadBytes {
		return "", fmt.Errorf("payload exceeds max size of %d bytes", e.limits.MaxPayloadBytes)
	}

	// This is the worker's local backpressure point.
	// If every execution slot is busy, reject quickly instead of queueing unlimited work.
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return "", fmt.Errorf("worker is at max execution capacity of %d", e.limits.MaxConcurrentExecs)
	}

	// The execution timeout covers the full worker-side execution path:
	// fetch if needed, instantiate, memory write, run, memory read, and cleanup.
	execCtx, cancel := context.WithTimeout(ctx, e.limits.ExecutionTimeout)
	defer cancel()

	// Make sure the module is compiled and available.
	// FetchAndCache downloads only on a miss for the module's digest-aware cache key.
	if err := e.FetchAndCacheWithDigest(execCtx, moduleName, moduleURL, moduleDigest, bearerToken); err != nil {
		return "", err
	}

	// Pull the compiled module out of the cache.
	// A compiled module is like a reusable template. It is not the running instance yet.
	cacheKeyString := ModuleCacheKey{
		ModuleName: strings.TrimSpace(moduleName),
		ModuleURL:  strings.TrimSpace(moduleURL),
		Digest:     strings.TrimSpace(moduleDigest),
	}.String()
	compiled, ok := e.cachedCompiledModule(cacheKeyString, time.Now())
	if !ok {
		return "", fmt.Errorf("module %s not loaded", moduleName)
	}

	// Instantiate a fresh module for this execution.
	// Each request gets its own instance and its own linear memory, so requests do not share data.
	mod, err := e.runtime.InstantiateModule(execCtx, compiled, wazero.NewModuleConfig())
	if err != nil {
		return "", fmt.Errorf("instantiate module %s: %w", moduleName, err)
	}
	defer mod.Close(execCtx)

	// Step 4: copy the input string into the module's linear memory.
	// Go memory and Wasm memory are separate, so we cannot pass a Go string directly.
	inputPtr, err := WriteString(execCtx, mod, payload)
	if err != nil {
		return "", fmt.Errorf("write input for %s: %w", moduleName, err)
	}

	// Find the exported entrypoint.
	// Expect modules to export:
	//   run(ptr uint32, len uint32) uint64
	// The uint64 packs the output pointer in the high 32 bits and output length in the low 32 bits.
	run := mod.ExportedFunction("run")
	if run == nil {
		return "", fmt.Errorf("module %s does not export run", moduleName)
	}

	// Call the Wasm function.
	// Wazero uses uint64 values for all Wasm parameters/results at the Go API boundary.
	results, err := run.Call(execCtx, uint64(inputPtr), uint64(len(payload)))
	if err != nil {
		return "", fmt.Errorf("run module %s: %w", moduleName, err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("module %s returned no result", moduleName)
	}

	// Unpack the pointer and length returned by the module.
	// This keeps the first ABI small because one Wasm result can carry both values.
	packedOutput := results[0]
	outputPtr := uint32(packedOutput >> 32)
	outputLen := uint32(packedOutput)
	if outputLen > e.limits.MaxOutputBytes {
		return "", fmt.Errorf("output exceeds max size of %d bytes", e.limits.MaxOutputBytes)
	}

	// Read the output bytes back out of Wasm memory and return them as a Go string.
	output, err := ReadString(mod, outputPtr, outputLen)
	if err != nil {
		return "", fmt.Errorf("read output for %s: %w", moduleName, err)
	}

	slog.Info("wasm execution completed", "module_name", moduleName, "duration_ms", time.Since(start).Milliseconds(), "output_bytes", len(output))
	return output, nil
}

func (e *WasmEngine) downloadAndCompile(ctx context.Context, moduleName, moduleURL string, moduleDigest string, bearerToken string) (wazero.CompiledModule, []byte, error) {
	// Module fetching has its own timeout.
	// This keeps slow storage or a stuck registry from holding a worker request forever.
	fetchCtx, cancel := context.WithTimeout(ctx, e.limits.ModuleFetchTimeout)
	defer cancel()

	// Tie the download request to the execution context.
	// If the caller cancels the request or adds a timeout later, the download stops too.
	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, moduleURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create request for %s: %w", moduleURL, err)
	}
	if bearerToken != "" {
		// Private registries like ACR expect the pull token in the Authorization header.
		// The master mints this token and forwards it to the worker as JITBearerToken.
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	// Download the raw .wasm bytes.
	// GET is safe to retry because it does not execute the module or mutate remote state.
	resp, err := shared.DoWithRetry(e.client, req)
	if err != nil {
		return nil, nil, fmt.Errorf("download %s: %w", moduleURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("download %s: unexpected status %s", moduleURL, resp.Status)
	}

	// Read only up to the configured module size plus one byte.
	// The extra byte tells us the module is too large without needing to read the whole body.
	limitedBody := io.LimitReader(resp.Body, e.limits.MaxModuleBytes+1)
	wasmBytes, err := io.ReadAll(limitedBody)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", moduleURL, err)
	}
	if int64(len(wasmBytes)) > e.limits.MaxModuleBytes {
		return nil, nil, fmt.Errorf("module %s exceeds max size of %d bytes", moduleName, e.limits.MaxModuleBytes)
	}
	if err := verifyModuleDigest(moduleName, wasmBytes, moduleDigest); err != nil {
		return nil, nil, err
	}

	// Compile once and cache the compiled form.
	// Compilation is more expensive than instantiation, so the cache avoids doing it for every request.
	compiled, err := e.runtime.CompileModule(fetchCtx, wasmBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("compile %s: %w", moduleName, err)
	}

	return compiled, wasmBytes, nil
}

func verifyModuleDigest(moduleName string, wasmBytes []byte, moduleDigest string) error {
	moduleDigest = strings.TrimSpace(moduleDigest)
	if moduleDigest == "" {
		return nil
	}

	algorithm, expectedDigest, ok := strings.Cut(moduleDigest, ":")
	if !ok || algorithm != "sha256" || expectedDigest == "" {
		return fmt.Errorf("unsupported module digest %q for %s", moduleDigest, moduleName)
	}
	expectedDigest = strings.ToLower(strings.TrimSpace(expectedDigest))
	if len(expectedDigest) != sha256.Size*2 {
		return fmt.Errorf("module digest %q for %s must be a sha256 hex digest", moduleDigest, moduleName)
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil {
		return fmt.Errorf("module digest %q for %s must be valid hex: %w", moduleDigest, moduleName, err)
	}

	actual := sha256.Sum256(wasmBytes)
	actualDigest := hex.EncodeToString(actual[:])
	if actualDigest != expectedDigest {
		return fmt.Errorf("module %s digest mismatch: expected sha256:%s got sha256:%s", moduleName, expectedDigest, actualDigest)
	}

	return nil
}

func (e *WasmEngine) cachedCompiledModule(cacheKey string, now time.Time) (wazero.CompiledModule, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	entry, ok := e.cache[cacheKey]
	if !ok {
		return nil, false
	}
	if e.entryExpired(entry, now) {
		e.removeEntryLocked(context.Background(), cacheKey, entry, "ttl_expired")
		return nil, false
	}
	entry.lastUsedAt = now
	e.cacheClock++
	entry.usedOrder = e.cacheClock
	e.cache[cacheKey] = entry

	return entry.compiled, true
}

func (e *WasmEngine) touchCachedModule(cacheKey string, now time.Time) bool {
	_, ok := e.cachedCompiledModule(cacheKey, now)
	return ok
}

func (e *WasmEngine) beginCompile(cacheKey string) (<-chan struct{}, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if done, exists := e.inflight[cacheKey]; exists {
		return done, false
	}

	done := make(chan struct{})
	e.inflight[cacheKey] = done
	return done, true
}

func (e *WasmEngine) finishCompile(cacheKey string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if done, exists := e.inflight[cacheKey]; exists {
		close(done)
		delete(e.inflight, cacheKey)
	}
}

func (e *WasmEngine) evictLocked(ctx context.Context, now time.Time) {
	for key, entry := range e.cache {
		if e.entryExpired(entry, now) {
			e.removeEntryLocked(ctx, key, entry, "ttl_expired")
		}
	}

	for len(e.cache) > e.limits.MaxCachedModules {
		key, entry, ok := e.oldestEntryLocked()
		if !ok {
			return
		}
		e.removeEntryLocked(ctx, key, entry, "max_cached_modules")
	}

	for e.cacheBytes > e.limits.MaxCacheBytes {
		key, entry, ok := e.oldestEntryLocked()
		if !ok {
			return
		}
		e.removeEntryLocked(ctx, key, entry, "max_cache_bytes")
	}
}

func (e *WasmEngine) entryExpired(entry moduleCacheEntry, now time.Time) bool {
	return e.limits.ModuleCacheTTL > 0 && now.Sub(entry.createdAt) > e.limits.ModuleCacheTTL
}

func (e *WasmEngine) oldestEntryLocked() (string, moduleCacheEntry, bool) {
	var oldestKey string
	var oldestEntry moduleCacheEntry
	found := false

	for key, entry := range e.cache {
		if !found || entry.usedOrder < oldestEntry.usedOrder {
			oldestKey = key
			oldestEntry = entry
			found = true
		}
	}

	return oldestKey, oldestEntry, found
}

func (e *WasmEngine) removeEntryLocked(ctx context.Context, key string, entry moduleCacheEntry, reason string) {
	delete(e.cache, key)
	e.cacheBytes -= entry.sizeBytes
	if e.cacheBytes < 0 {
		e.cacheBytes = 0
	}
	if err := entry.compiled.Close(ctx); err != nil {
		slog.Warn("module cache close failed", "module_name", entry.key.ModuleName, "module_digest", entry.key.Digest, "reason", reason, "error", err)
	}
	slog.Info("module cache entry evicted", "module_name", entry.key.ModuleName, "module_digest", entry.key.Digest, "reason", reason, "cache_entries", len(e.cache), "cache_bytes", e.cacheBytes)
}
