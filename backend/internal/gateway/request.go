package gateway

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func singleHeader(r *http.Request, key string) (string, error) {
	values := r.Header.Values(key)
	if len(values) != 1 || values[0] == "" {
		return "", ErrCapability
	}
	return values[0], nil
}
func positiveHeader(r *http.Request, key string) (int64, error) {
	raw, err := singleHeader(r, key)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 || strconv.FormatInt(value, 10) != raw {
		return 0, ErrCapability
	}
	return value, nil
}

// AuthorizeRequest combines certificate, signed scope and live DB authorization.
// URL/header identity must agree with signed identity and the canonical DB row.
func (h *HTTP) AuthorizeRequest(r *http.Request, scope string) (TaskContext, error) {
	cert, err := VerifiedPeer(r.TLS)
	if err != nil {
		return TaskContext{}, err
	}
	task, err := canonicalUUID(r.PathValue("task"))
	if err != nil {
		return TaskContext{}, err
	}
	workerRaw, err := singleHeader(r, "X-Photo-Worker")
	if err != nil {
		return TaskContext{}, err
	}
	worker, err := canonicalUUID(workerRaw)
	if err != nil {
		return TaskContext{}, err
	}
	attempt, err := positiveHeader(r, "X-Photo-Attempt")
	if err != nil || attempt > 6 {
		return TaskContext{}, ErrCapability
	}
	fence, err := positiveHeader(r, "X-Photo-Fence")
	if err != nil {
		return TaskContext{}, err
	}
	bearer, err := singleHeader(r, "Authorization")
	if err != nil || len(bearer) > 8199 || !strings.HasPrefix(bearer, "Bearer ") || h.Tasks == nil {
		return TaskContext{}, ErrCapability
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resolved, err := h.Tasks.Resolve(ctx, task, int(attempt), fence, worker, cert)
	if err != nil {
		return TaskContext{}, err
	}
	id := resolved.Identity
	if id.TaskID != task || id.WorkerID != worker || id.Attempt != int(attempt) || id.Fence != fence {
		return TaskContext{}, ErrCapability
	}
	if _, err = h.Signer.Verify(strings.TrimPrefix(bearer, "Bearer "), cert, id, scope); err != nil {
		return TaskContext{}, err
	}
	return resolved, nil
}
