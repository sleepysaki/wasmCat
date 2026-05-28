package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWasmEngineExecute(t *testing.T) {
	ctx := context.Background()

	// This tiny module is the first wasmCat execution contract in binary form.
	// Written as WAT, it is roughly:
	//
	// (module
	//   (memory (export "memory") 1)
	//   (global $heap (mut i32) (i32.const 1024))
	//   (func (export "malloc") (param $size i32) (result i32)
	//     global.get $heap
	//     global.get $heap
	//     local.get $size
	//     i32.add
	//     global.set $heap)
	//   (func (export "run") (param $ptr i32) (param $len i32) (result i64)
	//     local.get $ptr
	//     i64.extend_i32_u
	//     i64.const 32
	//     i64.shl
	//     local.get $len
	//     i64.extend_i32_u
	//     i64.or))
	//
	// malloc gives the host a safe place to write the input string.
	// run echoes the input by returning the same pointer and length packed into one i64.
	wasmEchoModule := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x0c, 0x02, 0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
		0x03, 0x03, 0x02, 0x00, 0x01,
		0x05, 0x03, 0x01, 0x00, 0x01,
		0x06, 0x07, 0x01, 0x7f, 0x01, 0x41, 0x80, 0x08, 0x0b,
		0x07, 0x19, 0x03,
		0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00,
		0x06, 0x6d, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00,
		0x03, 0x72, 0x75, 0x6e, 0x00, 0x01,
		0x0a, 0x1a, 0x02,
		0x0b, 0x00, 0x23, 0x00, 0x23, 0x00, 0x20, 0x00, 0x6a, 0x24, 0x00, 0x0b,
		0x0c, 0x00, 0x20, 0x00, 0xad, 0x42, 0x20, 0x86, 0x20, 0x01, 0xad, 0x84, 0x0b,
	}

	// Serve the module over HTTP because production workers fetch modules by URL.
	// This keeps the test close to the real FetchAndCache path without needing external storage.
	var downloads int
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads++
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule)
	}))
	defer moduleServer.Close()

	engine := NewWasmEngine(ctx)

	// First execution downloads, compiles, instantiates, writes input memory, calls run, and reads output memory.
	result, err := engine.Execute(ctx, "echo", moduleServer.URL, "hello wasm", "")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result != "hello wasm" {
		t.Fatalf("expected echo result, got %q", result)
	}

	// Second execution should use the compiled module cache.
	// It still gets a fresh instance, but it should not download the same module again.
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

	// This does not need to be a valid Wasm module because this test checks the HTTP fetch layer.
	// The server returns 401 unless the worker sends the bearer token from the master.
	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Write([]byte("not really wasm"))
	}))
	defer moduleServer.Close()

	engine := NewWasmEngine(ctx)
	err := engine.FetchAndCache(ctx, "private-module", moduleServer.URL, "test-token")
	if err == nil {
		t.Fatal("expected compile error after authorized download")
	}
	if !strings.Contains(err.Error(), "compile private-module") {
		t.Fatalf("expected authorized fetch to reach compile step, got %v", err)
	}
}
