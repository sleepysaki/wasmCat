package worker

import "time"

type Limits struct {
	ExecutionTimeout   time.Duration
	ModuleFetchTimeout time.Duration
	MaxModuleBytes     int64
	MaxPayloadBytes    int64
	MaxOutputBytes     uint32
	MaxConcurrentExecs int
	MaxCachedModules   int
	MaxCacheBytes      int64
	ModuleCacheTTL     time.Duration
}

var DefaultLimits = Limits{
	ExecutionTimeout:   5 * time.Second,
	ModuleFetchTimeout: 10 * time.Second,
	MaxModuleBytes:     10 << 20, // 10 MiB
	MaxPayloadBytes:    1 << 20,  // 1 MiB
	MaxOutputBytes:     1 << 20,  // 1 MiB
	MaxConcurrentExecs: 4,
	MaxCachedModules:   128,
	MaxCacheBytes:      256 << 20, // 256 MiB
	ModuleCacheTTL:     30 * time.Minute,
}

func normalizeLimits(limits Limits) Limits {
	// Keep each value usable even if a caller builds a partial Limits struct.
	// This lets tests override one limit without repeating every default value.
	if limits.ExecutionTimeout <= 0 {
		limits.ExecutionTimeout = DefaultLimits.ExecutionTimeout
	}
	if limits.ModuleFetchTimeout <= 0 {
		limits.ModuleFetchTimeout = DefaultLimits.ModuleFetchTimeout
	}
	if limits.MaxModuleBytes <= 0 {
		limits.MaxModuleBytes = DefaultLimits.MaxModuleBytes
	}
	if limits.MaxPayloadBytes <= 0 {
		limits.MaxPayloadBytes = DefaultLimits.MaxPayloadBytes
	}
	if limits.MaxOutputBytes == 0 {
		limits.MaxOutputBytes = DefaultLimits.MaxOutputBytes
	}
	if limits.MaxConcurrentExecs <= 0 {
		limits.MaxConcurrentExecs = DefaultLimits.MaxConcurrentExecs
	}
	if limits.MaxCachedModules <= 0 {
		limits.MaxCachedModules = DefaultLimits.MaxCachedModules
	}
	if limits.MaxCacheBytes <= 0 {
		limits.MaxCacheBytes = DefaultLimits.MaxCacheBytes
	}
	if limits.ModuleCacheTTL <= 0 {
		limits.ModuleCacheTTL = DefaultLimits.ModuleCacheTTL
	}

	return limits
}
