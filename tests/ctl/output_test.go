package ctl_test

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"wasmcat/internal/ctl"
	"wasmcat/internal/shared"
)

func TestWriteWorkersTable(t *testing.T) {
	var out bytes.Buffer
	workers := []shared.WorkerNode{{
		ID:        "worker-vn-01",
		IPAddress: "localhost:7271",
		State:     shared.WorkerStateReady,
		CPUFree:   88.5,
		RAMFreeMB: 4096,
		LastSeen:  time.Now(),
	}}

	if err := ctl.WriteWorkers(&out, workers); err != nil {
		t.Fatalf("write workers: %v", err)
	}

	text := out.String()
	for _, expected := range []string{"ID", "worker-vn-01", "localhost:7271", "88.5%", "4096 MB"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in output:\n%s", expected, text)
		}
	}
}

func TestWriteValueJSON(t *testing.T) {
	var out bytes.Buffer

	if err := ctl.WriteValue(&out, "json", shared.HealthResponse{Status: "ok", Role: "master"}); err != nil {
		t.Fatalf("write json: %v", err)
	}
	if !strings.Contains(out.String(), `"status": "ok"`) {
		t.Fatalf("unexpected json output: %s", out.String())
	}
}
