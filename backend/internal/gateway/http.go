package gateway

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

// TaskResolver must check live DB state on every request, including revocation.
type TaskResolver interface {
	Resolve(context.Context, uuid.UUID, int, int64, uuid.UUID, *x509.Certificate) (TaskContext, error)
}
type HTTP struct {
	Tasks            TaskResolver
	Signer           CapabilitySigner
	Inputs           InputResolver
	Objects          ObjectReader
	Bucket           string
	MaxDownloadBytes int64
	MaxUploadBytes   int64
	Uploads          *UploadService
}
type credentialsResponse struct {
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	ExpiresAt time.Time    `json:"expires_at"`
	Identity  TaskIdentity `json:"identity"`
}

func (h *HTTP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/tasks/{task}/credentials", h.credentials)
	mux.HandleFunc("GET /internal/v1/tasks/{task}/objects/{artifact}", h.download)
	mux.HandleFunc("PUT /internal/v1/tasks/{task}/outputs/{role}", h.upload)
	return mux
}
func gatewayError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{code})
}
func canonicalUUID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || raw != id.String() {
		return uuid.Nil, ErrCapability
	}
	return id, nil
}

// Duplicates and unknown keys are rejected before typed values are accepted.
// The decimal fence is a string to preserve full int64 precision in clients.
func credentialBody(r io.Reader) (int, int64, error) {
	d := json.NewDecoder(r)
	opening, err := d.Token()
	if err != nil || opening != json.Delim('{') {
		return 0, 0, ErrCapability
	}
	seen := map[string]bool{}
	attempt := 0
	var fence int64
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return 0, 0, ErrCapability
		}
		seen[key] = true
		switch key {
		case "attempt":
			if d.Decode(&attempt) != nil || attempt < 1 || attempt > 6 {
				return 0, 0, ErrCapability
			}
		case "fence":
			var raw string
			if d.Decode(&raw) != nil {
				return 0, 0, ErrCapability
			}
			fence, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || fence < 1 || strconv.FormatInt(fence, 10) != raw {
				return 0, 0, ErrCapability
			}
		default:
			return 0, 0, ErrCapability
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(seen) != 2 {
		return 0, 0, ErrCapability
	}
	if _, err = d.Token(); err != io.EOF {
		return 0, 0, ErrCapability
	}
	return attempt, fence, nil
}
func (h *HTTP) credentials(w http.ResponseWriter, r *http.Request) {
	cert, err := VerifiedPeer(r.TLS)
	if err != nil {
		gatewayError(w, 401, "INVALID_TASK_CAPABILITY")
		return
	}
	task, err := canonicalUUID(r.PathValue("task"))
	workers := r.Header.Values("X-Photo-Worker")
	if err != nil || len(workers) != 1 {
		gatewayError(w, 400, "VALIDATION_FAILED")
		return
	}
	worker, err := canonicalUUID(workers[0])
	if err != nil {
		gatewayError(w, 400, "VALIDATION_FAILED")
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		gatewayError(w, 415, "UNSUPPORTED_MEDIA_TYPE")
		return
	}
	if r.ContentLength > 1024 {
		gatewayError(w, 413, "BODY_TOO_LARGE")
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer controller.SetReadDeadline(time.Time{})
	attempt, fence, err := credentialBody(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		gatewayError(w, 400, "INVALID_JSON")
		return
	}
	if h.Tasks == nil {
		gatewayError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resolved, err := h.Tasks.Resolve(ctx, task, attempt, fence, worker, cert)
	if err != nil {
		if errors.Is(err, ErrCapability) {
			gatewayError(w, 403, "LEASE_LOST")
		} else {
			gatewayError(w, 503, "DEPENDENCY_UNAVAILABLE")
		}
		return
	}
	id := resolved.Identity
	if id.TaskID != task || id.WorkerID != worker || id.Attempt != attempt || id.Fence != fence {
		gatewayError(w, 403, "INVALID_TASK_CAPABILITY")
		return
	}
	raw, err := h.Signer.Issue(id, cert, resolved.LeaseUntil, resolved.Deadline, []string{ReadObjects, WriteOutputs})
	if err != nil {
		gatewayError(w, 403, "INVALID_TASK_CAPABILITY")
		return
	}
	claims, err := h.Signer.Verify(raw, cert, id, ReadObjects)
	if err != nil {
		gatewayError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(credentialsResponse{raw, "Bearer", claims.ExpiresAt.Time, id})
}
