package proxy

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/elvinaqalarov99/spectra/inference"
)

// Proxy is a transparent HTTP proxy that captures traffic and feeds it to the merger
type Proxy struct {
	target  *url.URL
	merger  *inference.SpecMerger
	client  *http.Client
	OnObs   func(*inference.Observation) // called after each observation is ingested
}

// New creates a new proxy that forwards requests to target and reports observations to merger
func New(target string, merger *inference.SpecMerger) (*Proxy, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		target: u,
		merger: merger,
		client: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Build upstream URL
	upstreamURL := *p.target
	upstreamURL.Path = singleJoiningSlash(p.target.Path, r.URL.Path)
	upstreamURL.RawQuery = r.URL.RawQuery

	// Read request body
	var reqBody []byte
	if r.Body != nil {
		var err error
		reqBody, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadGateway)
			return
		}
		r.Body.Close()
	}

	// Build forwarded request
	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "failed to build upstream request", http.StatusBadGateway)
		return
	}
	copyHeaders(upstreamReq.Header, r.Header)
	upstreamReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	upstreamReq.Header.Del("Accept-Encoding") // simplify: disable compression

	// Execute
	start := time.Now()
	resp, err := p.client.Do(upstreamReq)
	if err != nil {
		log.Printf("proxy: upstream error: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)
	_ = elapsed

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed to read response", http.StatusBadGateway)
		return
	}

	// Forward response to client
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	// Build and ingest observation asynchronously
	obs := &inference.Observation{
		Method:      r.Method,
		RawPath:     r.URL.Path,
		RequestBody: reqBody,
		StatusCode:  resp.StatusCode,
		ResponseBody: respBody,
		ContentType: r.Header.Get("Content-Type"),
		QueryParams: parseQueryParams(r.URL.RawQuery),
	}

	go func() {
		p.merger.Ingest(obs)
		if p.OnObs != nil {
			p.OnObs(obs)
		}
	}()
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		// Skip hop-by-hop headers
		switch strings.ToLower(k) {
		case "connection", "transfer-encoding", "keep-alive",
			"proxy-authenticate", "proxy-authorization",
			"te", "trailers", "upgrade":
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func singleJoiningSlash(a, b string) string {
	aSlash := strings.HasSuffix(a, "/")
	bSlash := strings.HasPrefix(b, "/")
	switch {
	case aSlash && bSlash:
		return a + b[1:]
	case !aSlash && !bSlash:
		return a + "/" + b
	}
	return a + b
}

func parseQueryParams(raw string) map[string]string {
	result := map[string]string{}
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return result
	}
	for k, v := range vals {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}
