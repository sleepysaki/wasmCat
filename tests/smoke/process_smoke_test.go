package smoke_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"wasmcat/internal/security"
	"wasmcat/internal/shared"
)

func TestMasterWorkerBinariesExecuteWASMOverMTLS(t *testing.T) {
	if testing.Short() {
		t.Skip("process smoke test is skipped in short mode")
	}

	repoRoot := findRepoRoot(t)
	tempDir := t.TempDir()
	certDir := filepath.Join(tempDir, "certs")
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create binary dir: %v", err)
	}

	masterBinary := filepath.Join(binDir, binaryName("wasmcat-master"))
	workerBinary := filepath.Join(binDir, binaryName("wasmcat-worker"))
	buildBinary(t, repoRoot, "./cmd/master", masterBinary)
	buildBinary(t, repoRoot, "./cmd/worker", workerBinary)

	moduleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/wasm")
		w.Write(wasmEchoModule())
	}))
	defer moduleServer.Close()

	workerID := "worker-smoke-01"
	masterPort := freePort(t)
	workerPort := freePort(t)

	master := startProcess(t, repoRoot, masterBinary, []string{
		"MASTER_PORT=" + masterPort,
		"CERT_DIR=" + certDir,
		"AUTO_GENERATE_CERTS=true",
		"DEV_WORKER_ID=" + workerID,
		"CLEANUP_INTERVAL=1s",
		"JOB_STORE_PATH=" + filepath.Join(tempDir, "wasmcat-jobs.db"),
		"MIN_WORKER_CPU_FREE=0",
		"MIN_WORKER_RAM_FREE_MB=0",
	})
	defer master.stop(t)

	waitForFile(t, security.WorkerCertPath(certDir, workerID), 5*time.Second)
	waitForFile(t, security.WorkerKeyPath(certDir, workerID), 5*time.Second)
	client := newSmokeMTLSClient(t, certDir, workerID)
	waitForReady(t, client, fmt.Sprintf("https://localhost:%s/wasmcat/ready", masterPort), master, 10*time.Second)

	worker := startProcess(t, repoRoot, workerBinary, []string{
		"WORKER_ID=" + workerID,
		"WORKER_PORT=" + workerPort,
		"MASTER_URL=https://localhost:" + masterPort,
		"WORKER_ADVERTISE_ADDRESS=localhost:" + workerPort,
		"WORKER_LATITUDE=13.7563",
		"WORKER_LONGITUDE=100.5018",
		"CERT_DIR=" + certDir,
		"HEARTBEAT_INTERVAL=250ms",
		"EXECUTION_TIMEOUT=5s",
		"MODULE_FETCH_TIMEOUT=5s",
		"MAX_MODULE_BYTES=1048576",
		"MAX_PAYLOAD_BYTES=1048576",
		"MAX_OUTPUT_BYTES=1048576",
		"MAX_CONCURRENT_EXECS=2",
		"MAX_CACHED_MODULES=8",
		"MAX_CACHE_BYTES=8388608",
		"MODULE_CACHE_TTL=5m",
	})
	defer worker.stop(t)

	waitForReady(t, client, fmt.Sprintf("https://localhost:%s/wasmcat/ready", workerPort), worker, 10*time.Second)

	executeURL := fmt.Sprintf("https://localhost:%s/api/v1/execute", masterPort)
	request := shared.ExecutionRequest{
		ModuleName: "echo-smoke",
		ModuleURL:  moduleServer.URL,
		Payload:    "real process smoke test",
		UserLat:    13.7563,
		UserLon:    100.5018,
	}

	response := executeWithRetry(t, client, executeURL, request, master, worker, 10*time.Second)
	if response.Result != request.Payload {
		t.Fatalf("expected result %q, got %q", request.Payload, response.Result)
	}
	if response.ExecutedOnNodeID != workerID {
		t.Fatalf("expected execution on %q, got %q", workerID, response.ExecutedOnNodeID)
	}
}

func buildBinary(t *testing.T, repoRoot string, packagePath string, outputPath string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", outputPath, packagePath)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, string(output))
	}
}

type runningProcess struct {
	name   string
	cancel context.CancelFunc
	done   chan error
	output *safeBuffer
}

func startProcess(t *testing.T, repoRoot string, binaryPath string, env []string) *runningProcess {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	buffer := &safeBuffer{}
	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = buffer
	cmd.Stderr = buffer

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start %s: %v", binaryPath, err)
	}

	process := &runningProcess{
		name:   filepath.Base(binaryPath),
		cancel: cancel,
		done:   make(chan error, 1),
		output: buffer,
	}
	go func() {
		process.done <- cmd.Wait()
	}()

	return process
}

func (p *runningProcess) stop(t *testing.T) {
	t.Helper()

	p.cancel()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not stop after cancellation\n%s", p.name, p.output.String())
	}
}

func (p *runningProcess) assertRunning(t *testing.T) {
	t.Helper()

	select {
	case err := <-p.done:
		t.Fatalf("%s exited early: %v\n%s", p.name, err, p.output.String())
	default:
	}
}

func waitForReady(t *testing.T, client *http.Client, url string, process *runningProcess, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		process.assertRunning(t)
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status %s", resp.Status)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("wait for ready %s: %v\n%s", url, lastErr, process.output.String())
}

func executeWithRetry(t *testing.T, client *http.Client, url string, request shared.ExecutionRequest, master *runningProcess, worker *runningProcess, timeout time.Duration) shared.ExecutionResponse {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastBody string
	var lastErr error
	for time.Now().Before(deadline) {
		master.assertRunning(t)
		worker.assertRunning(t)

		body, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		resp, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = err
			time.Sleep(150 * time.Millisecond)
			continue
		}

		var response shared.ExecutionResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&response)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && decodeErr == nil {
			return response
		}
		lastBody = fmt.Sprintf("status=%s decode=%v response=%+v", resp.Status, decodeErr, response)
		time.Sleep(150 * time.Millisecond)
	}

	t.Fatalf("execute never succeeded: %v %s\nmaster:\n%s\nworker:\n%s", lastErr, lastBody, master.output.String(), worker.output.String())
	return shared.ExecutionResponse{}
}

func newSmokeMTLSClient(t *testing.T, certDir string, workerID string) *http.Client {
	t.Helper()

	client, err := security.NewMTLSHTTPClient(security.WorkerCertPath(certDir, workerID), security.WorkerKeyPath(certDir, workerID), security.CACertPath(certDir))
	if err != nil {
		t.Fatalf("create smoke mTLS client: %v", err)
	}
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.TLSClientConfig.ServerName = "localhost"
		transport.TLSClientConfig.MinVersion = tls.VersionTLS12
	}

	return client
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("file %s was not created within %s", path, timeout)
}

func freePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer listener.Close()

	return fmt.Sprintf("%d", listener.Addr().(*net.TCPAddr).Port)
}

func binaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}

	return name
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root containing go.mod")
		}
		dir = parent
	}
}

type safeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func wasmEchoModule() []byte {
	return []byte{
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
}
