package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/elvinaqalarov99/spectra/inference"
)

// ingestPayload is the JSON body sent by SDK middlewares
type ingestPayload struct {
	Method       string            `json:"method"`
	RawPath      string            `json:"rawPath"`
	QueryParams  map[string]string `json:"queryParams"`
	RequestBody  string            `json:"requestBody"`
	StatusCode   int               `json:"statusCode"`
	ResponseBody string            `json:"responseBody"`
	ContentType  string            `json:"contentType"`
	DurationMs   int               `json:"durationMs"`
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20)) // 4 MB max
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var p ingestPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	obs := &inference.Observation{
		Method:       p.Method,
		RawPath:      p.RawPath,
		QueryParams:  p.QueryParams,
		RequestBody:  []byte(p.RequestBody),
		StatusCode:   p.StatusCode,
		ResponseBody: []byte(p.ResponseBody),
		ContentType:  p.ContentType,
	}

	s.merger.Ingest(obs)
	if s.OnObs != nil {
		s.OnObs(obs)
	}

	w.WriteHeader(http.StatusAccepted)
}
