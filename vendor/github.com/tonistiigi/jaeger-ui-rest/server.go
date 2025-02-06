package jaegerui

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/pkg/errors"
)

func NewServer(cfg Config) *Server {
	mux := &http.ServeMux{}
	s := &Server{
		config: cfg,
		server: &http.Server{
			Handler: mux,
		},
	}

	fsHandler := http.FileServer(FS(cfg))

	mux.HandleFunc("GET /api/services", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data": [], "total": 0}`))
	})
	mux.HandleFunc("GET /trace/", redirectRoot(fsHandler))
	mux.HandleFunc("GET /search", redirectRoot(fsHandler))

	mux.HandleFunc("POST /api/traces/", func(w http.ResponseWriter, r *http.Request) {
		traceID := strings.TrimPrefix(r.URL.Path, "/api/traces/")
		if traceID == "" || strings.Contains(traceID, "/") {
			http.Error(w, "Invalid trace ID", http.StatusBadRequest)
			return
		}
		handleHTTPError(s.AddTrace(traceID, r.Body), w)
	})

	mux.HandleFunc("GET /api/traces/", func(w http.ResponseWriter, r *http.Request) {
		traceID := strings.TrimPrefix(r.URL.Path, "/api/traces/")
		if traceID == "" {
			qry := r.URL.Query()
			ids := qry["traceID"]
			if len(ids) > 0 {
				dt, err := s.GetTraces(ids...)
				if err != nil {
					handleHTTPError(err, w)
					return
				}
				w.Write(dt)
				return
			}
		}

		if traceID == "" || strings.Contains(traceID, "/") {
			http.Error(w, "Invalid trace ID", http.StatusBadRequest)
			return
		}
		dt, err := s.GetTraces(traceID)
		if err != nil {
			handleHTTPError(err, w)
			return
		}
		w.Write(dt)
	})

	mux.Handle("/", fsHandler)

	return s
}

type Server struct {
	config Config
	server *http.Server

	mu     sync.Mutex
	traces map[string][]byte
}

func (s *Server) AddTrace(traceID string, rdr io.Reader) error {
	var payload struct {
		Data []struct {
			TraceID string `json:"traceID"`
		} `json:"data"`
	}
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, rdr); err != nil {
		return errors.Wrapf(err, "failed to read trace data")
	}
	dt := buf.Bytes()

	if err := json.Unmarshal(dt, &payload); err != nil {
		return errors.Wrapf(err, "failed to unmarshal trace data")
	}

	if len(payload.Data) != 1 {
		return errors.Errorf("expected 1 trace, got %d", len(payload.Data))
	}

	if payload.Data[0].TraceID != traceID {
		return errors.Errorf("trace ID mismatch: %s != %s", payload.Data[0].TraceID, traceID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.traces == nil {
		s.traces = make(map[string][]byte)
	}
	s.traces[traceID] = dt
	return nil
}

func (s *Server) GetTraces(traceIDs ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(traceIDs) == 0 {
		return nil, errors.Errorf("trace ID is required")
	}

	if len(traceIDs) == 1 {
		dt, ok := s.traces[traceIDs[0]]
		if !ok {
			return nil, errors.Wrapf(os.ErrNotExist, "trace %s not found", traceIDs[0])
		}
		return dt, nil
	}

	type payloadT struct {
		Data []interface{} `json:"data"`
	}
	var payload payloadT

	for _, traceID := range traceIDs {
		dt, ok := s.traces[traceID]
		if !ok {
			return nil, errors.Wrapf(os.ErrNotExist, "trace %s not found", traceID)
		}
		var p payloadT
		if err := json.Unmarshal(dt, &p); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal trace data")
		}
		payload.Data = append(payload.Data, p.Data...)
	}

	return json.MarshalIndent(payload, "", "  ")
}

func (s *Server) Serve(l net.Listener) error {
	return s.server.Serve(l)
}

func redirectRoot(h http.Handler) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/"
		h.ServeHTTP(w, r)
	}
}

func handleHTTPError(err error, w http.ResponseWriter) {
	switch {
	case err == nil:
		return
	case errors.Is(err, os.ErrNotExist):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}
