package shared

import (
	"os"
	"strings"
)

func DefaultWorkerAdvertiseAddress(port string) string {
	// The master calls the worker through this address, so localhost is only a safe
	// default when every process is on the same host. Prefer the OS hostname for
	// real installs, and keep localhost only as the last fallback.
	hostname, err := os.Hostname()
	if err == nil {
		hostname = strings.TrimSpace(hostname)
		if hostname != "" {
			return hostname + ":" + port
		}
	}

	return "localhost:" + port
}
