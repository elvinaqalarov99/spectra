package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// handleAPIProxy forwards /api-proxy/{path} to the configured backend URL.
// This lets Swagger UI Execute hit the real API without CORS issues — requests
// go to localhost:7878/api-proxy/... which Specula proxies to the backend.
func (s *Server) handleAPIProxy(w http.ResponseWriter, r *http.Request) {
	target := s.targetURL
	if target == "" {
		// Fall back to spec's first server URL
		spec := s.merger.Spec()
		if len(spec.Servers) > 0 {
			target = spec.Servers[0].URL
		}
	}
	if target == "" {
		http.Error(w, "no backend configured — start specula with --api or --target", http.StatusServiceUnavailable)
		return
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		http.Error(w, "invalid backend URL", http.StatusInternalServerError)
		return
	}

	// Strip the /api-proxy prefix so the request path matches the real API
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api-proxy")
	if r.URL.Path == "" {
		r.URL.Path = "/"
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
			// Strip the proxy prefix from any path prefix the backend has
			if targetURL.Path != "" && targetURL.Path != "/" {
				req.URL.Path = strings.TrimRight(targetURL.Path, "/") + req.URL.Path
			}
			// Remove headers that cause issues upstream
			req.Header.Del("X-Forwarded-For")
		},
		ModifyResponse: func(resp *http.Response) error {
			// Allow Swagger UI (running on localhost:7878) to read the response
			resp.Header.Set("Access-Control-Allow-Origin", "*")
			resp.Header.Set("Access-Control-Allow-Headers", "*")
			resp.Header.Set("Access-Control-Allow-Methods", "*")
			return nil
		},
	}

	proxy.ServeHTTP(w, r)
}
