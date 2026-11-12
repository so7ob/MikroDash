package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// subscriberBody validates the complete bounded request before command effects
// such as portal login, entitlement reservation or administrative mutations.
// Administrator authorization and origin checks still run first in the caller.
func subscriberBody(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	// JSON permits only these four whitespace bytes; Unicode trimming would
	// silently repair malformed commands before the decoder sees them.
	raw = bytes.TrimLeft(raw, " \t\r\n")
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
