package worker_test

import (
	"context"
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
