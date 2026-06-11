package worker_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wasmcat/internal/worker"
)

func TestWasmEngineExecute(t *testing.T) {
	ctx := context.Background()

	var downloads int
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)

	result, err := engine.Execute(ctx, "echo", moduleServer.URL, "hello wasm", "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result != "hello wasm" {
		t.Fatalf("expected echo result, got %q", result)
	}

	result, err = engine.Execute(ctx, "echo", moduleServer.URL, "cached", "")
	if err != nil {
		t.Fatalf("Execute from cache returned error: %v", err)
	}
	if result != "cached" {
		t.Fatalf("expected cached result, got %q", result)
	}
	if downloads != 1 {
		t.Fatalf("expected module to be downloaded once, downloaded %d times", downloads)
	}
}

func TestWasmEngineFetchAndCacheUsesBearerToken(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Write([]byte("not really wasm"))
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)
	err := engine.FetchAndCache(ctx, "private-module", moduleServer.URL, "test-token")
	if err == nil {
		t.Fatal("expected compile error after authorized download")
	}
	if !strings.Contains(err.Error(), "compile private-module") {
		t.Fatalf("expected authorized fetch to reach compile step, got %v", err)
	}
}

func TestWasmEngineVerifiesMatchingModuleDigest(t *testing.T) {
	ctx := context.Background()
	moduleBytes := wasmEchoModule()
	digest := sha256Digest(moduleBytes)

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(moduleBytes)
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)
	result, err := engine.ExecuteWithDigest(ctx, "echo", moduleServer.URL, digest, "verified", "")
	if err != nil {
		t.Fatalf("ExecuteWithDigest returned error: %v", err)
	}
	if result != "verified" {
		t.Fatalf("expected verified result, got %q", result)
	}
}

func TestWasmEngineRejectsMismatchedModuleDigest(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)
	err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "sha256:0000000000000000000000000000000000000000000000000000000000000000", "")
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected digest mismatch error, got %v", err)
	}
	var digestErr *worker.ModuleDigestError
	if !errors.As(err, &digestErr) {
		t.Fatalf("expected ModuleDigestError, got %T", err)
	}
	if digestErr.Reason != worker.ModuleDigestErrorMismatch {
		t.Fatalf("expected mismatch reason, got %q", digestErr.Reason)
	}
	if stats := engine.CacheStats(); stats.Entries != 0 {
		t.Fatalf("expected mismatched module not to be cached, got %+v", stats)
	}
}

func TestWasmEngineRejectsInvalidModuleDigestFormat(t *testing.T) {
	ctx := context.Background()

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	engine := worker.NewWasmEngine(ctx)
	err := engine.FetchAndCacheWithDigest(ctx, "echo", moduleServer.URL, "md5:abc", "")
	if err == nil {
		t.Fatal("expected invalid digest format error")
	}
	if !strings.Contains(err.Error(), "unsupported module digest") {
		t.Fatalf("expected unsupported module digest error, got %v", err)
	}
	var digestErr *worker.ModuleDigestError
	if !errors.As(err, &digestErr) {
		t.Fatalf("expected ModuleDigestError, got %T", err)
	}
	if digestErr.Reason != worker.ModuleDigestErrorInvalid {
		t.Fatalf("expected invalid reason, got %q", digestErr.Reason)
	}
}

func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
