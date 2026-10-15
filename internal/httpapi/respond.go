// Package httpapi serves the dashboard page and the /api/* endpoints.
//
// The response contracts live in docs/API.md and must not change: frontends,
// scripts and monitors already depend on them. Errors are always
// {"status":"error","message":"..."}, with an extra errors array for
// validation failures.
package httpapi

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// errorBody is the uniform shape of all error responses.
type errorBody struct {
	Status  string   `json:"status"`
	Message string   `json:"message,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"error","message":"Failed to encode the response"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Status: "error", Message: message})
}

func failValidation(w http.ResponseWriter, problems []string) {
	writeJSON(w, http.StatusBadRequest, errorBody{Status: "error", Errors: problems})
}

// ok is the generic success shape: always carries status=success; the caller adds the rest.
func ok(w http.ResponseWriter, fields map[string]any) {
	payload := make(map[string]any, len(fields)+1)
	payload["status"] = "success"
	for k, v := range fields {
		payload[k] = v
	}
	writeJSON(w, http.StatusOK, payload)
}

// etagJSON adds an ETag to read endpoints so polling clients get a bare 304 and skip the whole body.
func etagJSON(w http.ResponseWriter, r *http.Request, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		fail(w, http.StatusInternalServerError, "Failed to encode the response")
		return
	}
	sum := md5.Sum(body)
	etag := hex.EncodeToString(sum[:])

	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// decodeJSON reads the request body; anything that is not valid JSON gets a 400.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Body == nil {
		fail(w, http.StatusBadRequest, "The request body must be valid JSON")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil {
		fail(w, http.StatusBadRequest, "The request body must be valid JSON")
		return false
	}
	return true
}

// intParam reads a query parameter with range validation; out of range gets a 400.
func intParam(r *http.Request, name string, fallback, minValue, maxValue int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("Invalid parameter: %s must be an integer", name)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("Invalid parameter: %s must be between %d and %d", name, minValue, maxValue)
	}
	return value, nil
}

// cooldown is a mutex-plus-cooldown for heavy operations: one at a time, and
// none accepted for a few seconds after one completes.
//
// Manual refresh and immediate scans really hit upstream APIs; without this
// guard a few rapid clicks drain the quota or land the IP on the provider's
// throttle list.
type cooldown struct {
	seconds  int
	mu       sync.Mutex
	busy     bool
	lastDone time.Time
}

// acquire returns an empty string when granted, otherwise a user-facing reason.
func (c *cooldown) acquire() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.busy {
		return "already in progress; please wait"
	}
	if remaining := time.Duration(c.seconds)*time.Second - time.Since(c.lastDone); remaining > 0 {
		return fmt.Sprintf("too frequent; retry in %d seconds", max(1, int(remaining.Seconds())))
	}
	c.busy = true
	return ""
}

func (c *cooldown) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = false
	c.lastDone = time.Now()
}

// maskSecret masks a key for display.
func maskSecret(value string, prefix, suffix int) string {
	if value == "" {
		return ""
	}
	if len(value) <= prefix+suffix {
		return "***"
	}
	return value[:prefix] + "***" + value[len(value)-suffix:]
}
