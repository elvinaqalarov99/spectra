package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/elvinaqalarov99/specula/inference"
)

// ingestPayload is the JSON body sent by SDK middlewares.
// QueryParams and ResponseHeaders use json.RawMessage so we can tolerate
// PHP sending [] (empty array) instead of {} (empty object) for empty maps.
type ingestPayload struct {
	Method          string          `json:"method"`
	RawPath         string          `json:"rawPath"`
	QueryParams     json.RawMessage `json:"queryParams"`
	RequestBody     string          `json:"requestBody"`
	StatusCode      int             `json:"statusCode"`
	ResponseBody    string          `json:"responseBody"`
	ResponseHeaders json.RawMessage `json:"responseHeaders"`
	ContentType     string          `json:"contentType"`
	DurationMs      int             `json:"durationMs"`
	StripPrefix     string          `json:"stripPrefix"`
}

// decodeStringMap unmarshals a JSON value that is either an object {} or an
// empty array [] (PHP encodes empty associative arrays as []).
func decodeStringMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil {
		return m
	}
	// Was [] or some other non-object — treat as empty
	return nil
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
		Method:          p.Method,
		RawPath:         p.RawPath,
		QueryParams:     decodeStringMap(p.QueryParams),
		RequestBody:     []byte(p.RequestBody),
		StatusCode:      p.StatusCode,
		ResponseBody:    []byte(p.ResponseBody),
		ResponseHeaders: decodeStringMap(p.ResponseHeaders),
		ContentType:     p.ContentType,
		StripPrefix:     p.StripPrefix,
	}

	s.merger.Ingest(obs)
	if s.OnObs != nil {
		s.OnObs(obs)
	}

	w.WriteHeader(http.StatusAccepted)
}
