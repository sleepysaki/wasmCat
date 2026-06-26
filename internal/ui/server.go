package ui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"
	"wasmcat/internal/ctl"
	"wasmcat/internal/shared"
)

const (
	DefaultListenAddress = ":7280"
	DefaultAPITimeout    = 30 * time.Second
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	ConfigPath    string
	Timeout       time.Duration
	ClientFactory func() (*ctl.Client, error)
}

type configResponse struct {
	Path   string     `json:"path"`
	Exists bool       `json:"exists"`
	Config ctl.Config `json:"config"`
	Error  string     `json:"error,omitempty"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ui/api/config", s.handleConfig)
	mux.HandleFunc("/ui/api/health", s.forwardHealth)
	mux.HandleFunc("/ui/api/ready", s.forwardReady)
	mux.HandleFunc("/ui/api/metrics", s.forwardMetrics)
	mux.HandleFunc("/ui/api/workers", s.handleWorkers)
	mux.HandleFunc("/ui/api/workers/", s.handleWorkerAction)
	mux.HandleFunc("/ui/api/execute", s.handleExecute)
	mux.HandleFunc("/ui/api/jobs", s.handleJobs)
	mux.HandleFunc("/ui/api/jobs/", s.handleJob)
	mux.Handle("/", s.staticHandler())
	return mux
}

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusInternalServerError, err)
		})
	}

	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			data, err := staticFiles.ReadFile("static/index.html")
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		path, cfg, exists, err := s.loadConfigState()
		response := configResponse{Path: path, Exists: exists, Config: cfg}
		if err != nil {
			response.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPut:
		var cfg ctl.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode config: %w", err))
			return
		}
		if err := cfg.ValidateForRequest(); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := ctl.SaveConfig(s.ConfigPath, cfg); err != nil {
			writeError(w, http.StatusServiceUnavailable, err)
			return
		}
		path, loaded, exists, err := s.loadConfigState()
		response := configResponse{Path: path, Exists: exists, Config: loaded}
		if err != nil {
			response.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, response)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPut)
	}
}

func (s *Server) forwardHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.Health(ctx)
	})
}

func (s *Server) forwardReady(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.Ready(ctx)
	})
}

func (s *Server) forwardMetrics(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.Metrics(ctx)
	})
}

func (s *Server) handleWorkers(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.Workers(ctx)
	})
}

func (s *Server) handleWorkerAction(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/ui/api/workers/")
	workerID, action, ok := strings.Cut(rest, "/")
	if !ok || action != "drain" || strings.TrimSpace(workerID) == "" {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown worker action"))
		return
	}

	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.Drain(ctx, workerID)
	})
}

func (s *Server) handleExecute(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req shared.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode execution request: %w", err))
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.Execute(ctx, req)
	})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req shared.ExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode job request: %w", err))
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.CreateJob(ctx, req)
	})
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	requestID := strings.TrimPrefix(r.URL.Path, "/ui/api/jobs/")
	if strings.TrimSpace(requestID) == "" || strings.Contains(requestID, "/") {
		writeError(w, http.StatusBadRequest, fmt.Errorf("request_id is required"))
		return
	}

	s.forward(w, r, func(ctx context.Context, client *ctl.Client) (any, error) {
		return client.GetJob(ctx, requestID)
	})
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request, call func(context.Context, *ctl.Client) (any, error)) {
	client, err := s.client()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout())
	defer cancel()

	response, err := call(ctx, client)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) client() (*ctl.Client, error) {
	if s.ClientFactory != nil {
		return s.ClientFactory()
	}

	cfg, err := ctl.LoadConfig(s.ConfigPath)
	if err != nil {
		return nil, err
	}

	return ctl.NewClient(cfg)
}

func (s *Server) loadConfigState() (string, ctl.Config, bool, error) {
	path := s.ConfigPath
	if strings.TrimSpace(path) == "" {
		defaultPath, err := ctl.DefaultConfigPath()
		if err != nil {
			return "", ctl.Config{}, false, err
		}
		path = defaultPath
	}

	cfg, err := ctl.LoadConfig(path)
	if err == nil {
		return path, cfg, true, nil
	}
	if strings.Contains(err.Error(), "does not exist") {
		return path, ctl.DefaultLocalConfig(), false, err
	}

	return path, ctl.Config{}, false, err
}

func (s *Server) timeout() time.Duration {
	if s.Timeout <= 0 {
		return DefaultAPITimeout
	}

	return s.Timeout
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	writeJSON(w, status, map[string]string{
		"error": err.Error(),
	})
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	methodNotAllowed(w, method)
	return false
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
}
