package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// subscriberBody validates the complete bounded request before a caller can
// authenticate, reserve entitlement or commit an administrative change.
func subscriberBody(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	raw = bytes.TrimSpace(raw)
	if err != nil || len(raw) == 0 || raw[0] != '{' {
		writeJSONErr(w, 400, "Invalid request")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSONErr(w, 400, "Invalid request")
		return false
	}
	// Decode accepts a stream of JSON values; an HTTP command accepts one object.
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		writeJSONErr(w, 400, "Invalid request")
		return false
	}
	return true
}
