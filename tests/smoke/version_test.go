package smoke_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMasterWorkerVersionSubcommand verifies that the master and worker binaries
// report a version, and that the build-time -ldflags injection is wired up.
// The install-or-update.sh script relies on `wasmcat-master version` and
// `wasmcat-worker version` to show installed-vs-new versions during upgrades.
func TestMasterWorkerVersionSubcommand(t *testing.T) {
	if testing.Short() {
		t.Skip("version smoke test is skipped in short mode")
	}

	repoRoot := findRepoRoot(t)
	binDir := t.TempDir()

	cases := []struct {
		name    string
		pkg     string
		binary  string
		wantPfx string
	}{
		{"master", "./cmd/master", "wasmcat-master", "wasmcat-master "},
		{"worker", "./cmd/worker", "wasmcat-worker", "wasmcat-worker "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(binDir, binaryName(tc.binary))
			buildVersionBinary(t, repoRoot, tc.pkg, out, "9.9.9-test")

			got := runVersion(t, out)
			if !strings.HasPrefix(got, tc.wantPfx) {
				t.Fatalf("version output %q does not start with %q", got, tc.wantPfx)
			}
			if !strings.Contains(got, "9.9.9-test") {
				t.Fatalf("version output %q does not contain injected version", got)
			}
		})
	}
}

func buildVersionBinary(t *testing.T, repoRoot, pkg, out, version string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build",
		"-ldflags", "-X main.version="+version, "-o", out, pkg)
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, string(output))
	}
}

func runVersion(t *testing.T, binaryPath string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, binaryPath, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("run version: %v\n%s", err, string(output))
	}

	return strings.TrimSpace(string(output))
}
