package worker

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

type NodeMetrics struct {
	CPUFree   float64
	RAMFreeMB float64
}

type MetricsProvider interface {
	Snapshot(ctx context.Context) (NodeMetrics, error)
}

type SystemMetricsProvider struct{}

func (p SystemMetricsProvider) Snapshot(ctx context.Context) (NodeMetrics, error) {
	cpuUsedPercent, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return NodeMetrics{}, fmt.Errorf("read cpu percent: %w", err)
	}
	if len(cpuUsedPercent) == 0 {
		return NodeMetrics{}, fmt.Errorf("read cpu percent: no samples returned")
	}

	memory, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return NodeMetrics{}, fmt.Errorf("read virtual memory: %w", err)
	}

	return NodeMetrics{
		CPUFree:   100 - cpuUsedPercent[0],
		RAMFreeMB: float64(memory.Available) / 1024 / 1024,
	}, nil
}
