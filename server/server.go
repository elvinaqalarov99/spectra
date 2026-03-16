package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/elvinaqalarov99/specula/inference"
)

// Server exposes the live spec over HTTP and pushes updates over WebSocket
type Server struct {
	merger    *inference.SpecMerger
	hub       *wsHub
	mux       *http.ServeMux
	targetURL string // backend URL for /api-proxy pass-through
	OnObs     func(*inference.Observation) // called after each SDK ingest
}

func New(merger *inference.SpecMerger) *Server {
	s := &Server{
		merger: merger,
		hub:    newHub(),
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

// SetTarget configures the backend URL that /api-proxy forwards to.
func (s *Server) SetTarget(u string) {
	s.targetURL = u
}

func (s *Server) routes() {
	s.mux.HandleFunc("/spec", s.handleSpec)        // GET → fetch, DELETE → reset
	s.mux.HandleFunc("/spec.yaml", s.handleSpecYAML)
	s.mux.HandleFunc("/spec/reset", s.handleReset) // POST /spec/reset (explicit)
	s.mux.HandleFunc("/ws", s.handleWS)
	s.mux.HandleFunc("/ingest", s.handleIngest)
	s.mux.HandleFunc("/api-proxy/", s.handleAPIProxy) // pass-through for Swagger UI Execute
	s.mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	// Serve embedded Swagger UI
	s.mux.Handle("/docs/", http.StripPrefix("/docs/", swaggerUIHandler()))
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/docs/", http.StatusFound)
		}
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS for local dev
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleSpec(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, "":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.merger.Spec())
	case http.MethodDelete:
		s.handleReset(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	s.merger.Reset()
	log.Println("spec reset — all observations cleared")

	// Notify all connected UI clients so Swagger UI clears instantly
	data, _ := json.Marshal(map[string]interface{}{
		"event": "spec_reset",
		"spec":  s.merger.Spec(),
	})
	s.hub.broadcast(data)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
}

func (s *Server) handleSpecYAML(w http.ResponseWriter, r *http.Request) {
	// Return JSON with YAML content-type — most tools accept JSON OpenAPI
	w.Header().Set("Content-Type", "application/x-yaml")
	spec := s.merger.Spec()
	json.NewEncoder(w).Encode(spec)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	s.hub.ServeWS(w, r)
}

// NotifyUpdate broadcasts the current spec to all WebSocket clients.
// Also used as p.OnObs in the proxy.
func (s *Server) NotifyUpdate(obs *inference.Observation) {
	spec := s.merger.Spec()
	data, err := json.Marshal(map[string]interface{}{
		"event": "spec_update",
		"path":  obs.PathTemplate,
		"spec":  spec,
	})
	if err != nil {
		return
	}
	s.hub.broadcast(data)
}

func (s *Server) Listen(addr string) error {
	go s.hub.run()
	log.Printf("specula server listening on %s", addr)
	return http.ListenAndServe(addr, s)
}

// ---- WebSocket hub ----

type wsClient struct {
	conn interface{ WriteMessage(int, []byte) error }
	send chan []byte
}

type wsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]bool
	bcCh    chan []byte
}

func newHub() *wsHub {
	return &wsHub{
		clients: map[*wsClient]bool{},
		bcCh:    make(chan []byte, 256),
	}
}

func (h *wsHub) run() {
	for msg := range h.bcCh {
		h.mu.RLock()
		for c := range h.clients {
			select {
			case c.send <- msg:
			default:
			}
		}
		h.mu.RUnlock()
	}
}

func (h *wsHub) broadcast(msg []byte) {
	select {
	case h.bcCh <- msg:
	default:
		log.Println("ws hub: broadcast channel full, dropping message")
	}
}

func (h *wsHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	// Lightweight WebSocket upgrade without external dependency
	// Uses the golang.org/x/net/websocket approach via standard library
	// For a production build, swap in gorilla/websocket
	upgradeWebSocket(w, r, h)
}
