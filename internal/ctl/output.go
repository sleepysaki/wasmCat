package ctl

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"
	"wasmcat/internal/shared"
)

func WriteValue(w io.Writer, output string, value any) error {
	if output == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}

	switch typed := value.(type) {
	case shared.HealthResponse:
		_, err := fmt.Fprintf(w, "status=%s role=%s node_id=%s\n", typed.Status, typed.Role, typed.NodeID)
		return err
	case shared.ExecutionResponse:
		_, err := fmt.Fprintf(w, "request_id=%s\nnode=%s\nexecution_time_ms=%.3f\nresult=%s\n", typed.RequestID, typed.ExecutedOnNodeID, typed.ExecutionTimeMs, typed.Result)
		return err
	case shared.JobResponse:
		_, err := fmt.Fprintf(w, "request_id=%s\nstatus=%s\nworker=%s\nattempt=%d/%d\nlast_error=%s\n", typed.RequestID, typed.Status, typed.WorkerID, typed.Attempt, typed.MaxAttempts, typed.LastError)
		if err != nil {
			return err
		}
		if typed.Response != nil {
			_, err = fmt.Fprintf(w, "result=%s\n", typed.Response.Result)
		}
		return err
	case shared.APIResponse:
		_, err := fmt.Fprintf(w, "status=%s message=%s\n", typed.Status, typed.Message)
		return err
	case []shared.WorkerNode:
		return WriteWorkers(w, typed)
	default:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
}

func WriteWorkers(w io.Writer, workers []shared.WorkerNode) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tADDRESS\tSTATE\tCPU FREE\tRAM FREE\tLAST SEEN"); err != nil {
		return err
	}
	now := time.Now()
	for _, worker := range workers {
		state := worker.State
		if state == "" {
			state = shared.WorkerStateReady
		}
		lastSeen := "unknown"
		if !worker.LastSeen.IsZero() {
			age := now.Sub(worker.LastSeen)
			if age < 0 {
				age = 0
			}
			lastSeen = age.Round(time.Second).String() + " ago"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%.1f%%\t%.0f MB\t%s\n", worker.ID, worker.IPAddress, state, worker.CPUFree, worker.RAMFreeMB, lastSeen); err != nil {
			return err
		}
	}

	return tw.Flush()
}
