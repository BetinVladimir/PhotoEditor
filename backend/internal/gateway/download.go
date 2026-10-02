package gateway

import (
	"context"
	"crypto/x509"
	"errors"
	"github.com/google/uuid"
	"io"
	"net/http"
	"photoeditor/backend/internal/objectstore"
	"strconv"
	"strings"
	"time"
)

type InputResolver interface {
	Input(context.Context, TaskIdentity, *x509.Certificate, uuid.UUID, string) (objectstore.Object, error)
}
type ObjectReader interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
	OpenRange(context.Context, objectstore.Object, int64, int64) (io.ReadCloser, error)
}

const maxRangeBytes int64 = 16 * 1024 * 1024

// Single explicit byte ranges only; suffix/multiple/unbounded ranges are refused.
func boundedRange(raw string, size int64) (int64, int64, error) {
	if !strings.HasPrefix(raw, "bytes=") {
		return 0, 0, objectstore.ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(raw, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, objectstore.ErrInvalid
	}
	start, e1 := strconv.ParseInt(parts[0], 10, 64)
	end, e2 := strconv.ParseInt(parts[1], 10, 64)
	if e1 != nil || e2 != nil || start < 0 || end < start || end >= size || end-start >= maxRangeBytes || strconv.FormatInt(start, 10) != parts[0] || strconv.FormatInt(end, 10) != parts[1] {
		return 0, 0, objectstore.ErrInvalid
	}
	return start, end, nil
}
func (h *HTTP) download(w http.ResponseWriter, r *http.Request) {
	task, err := h.AuthorizeRequest(r, ReadObjects)
	if err != nil {
		downloadError(w, err)
		return
	}
	artifact, err := canonicalUUID(r.PathValue("artifact"))
	if err != nil {
		gatewayError(w, 404, "ARTIFACT_MISSING")
		return
	}
	if h.Inputs == nil || h.Objects == nil || h.Bucket == "" {
		gatewayError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	cert, err := VerifiedPeer(r.TLS)
	if err != nil {
		downloadError(w, err)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), task.Deadline)
	defer cancel()
	lookup, stop := context.WithTimeout(ctx, 5*time.Second)
	object, err := h.Inputs.Input(lookup, task.Identity, cert, artifact, h.Bucket)
	stop()
	if err != nil {
		downloadError(w, err)
		return
	}
	if h.MaxDownloadBytes <= 0 || object.Bytes < 0 || object.Bytes > h.MaxDownloadBytes {
		gatewayError(w, 413, "DISK_QUOTA")
		return
	}
	status := http.StatusOK
	length := object.Bytes
	var body io.ReadCloser
	ranges := r.Header.Values("Range")
	if len(ranges) > 0 {
		if len(ranges) != 1 {
			gatewayError(w, 416, "INVALID_RANGE")
			return
		}
		start, end, rangeErr := boundedRange(ranges[0], object.Bytes)
		if rangeErr != nil {
			w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(object.Bytes, 10))
			gatewayError(w, 416, "INVALID_RANGE")
			return
		}
		body, err = h.Objects.OpenRange(ctx, object, start, end)
		status = http.StatusPartialContent
		length = end - start + 1
		if err == nil {
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(object.Bytes, 10))
		}
	} else {
		body, err = h.Objects.Open(ctx, object)
	}
	if err != nil {
		downloadError(w, err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Photo-SHA256", object.SHA256)
	w.Header().Set("X-Photo-Artifact-ID", artifact.String())
	w.Header().Set("Accept-Ranges", "bytes")
	// Revalidate live DB state while streaming, without keeping a SQL transaction
	// open over network IO. Token expiry gates new requests; the DB lease gates this stream.
	controller := http.NewResponseController(w)
	defer controller.SetWriteDeadline(time.Time{})
	nextCheck := time.Time{}
	remaining := length
	buffer := make([]byte, 64*1024)
	w.WriteHeader(status)
	for {
		if !time.Now().Before(nextCheck) {
			checkCtx, done := context.WithTimeout(ctx, 5*time.Second)
			current, e := h.Tasks.Resolve(checkCtx, task.Identity.TaskID, task.Identity.Attempt, task.Identity.Fence, task.Identity.WorkerID, cert)
			done()
			if e != nil || current.Identity != task.Identity {
				panic(http.ErrAbortHandler)
			}
			nextCheck = time.Now().Add(5 * time.Second)
		}
		n, e := body.Read(buffer)
		if int64(n) > remaining {
			panic(http.ErrAbortHandler)
		}
		if n > 0 {
			_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return
			}
			remaining -= int64(n)
		}
		if e == io.EOF {
			if remaining != 0 {
				panic(http.ErrAbortHandler)
			}
			return
		}
		if e != nil {
			panic(http.ErrAbortHandler)
		}
	}
}
func downloadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCapability):
		gatewayError(w, 403, "LEASE_LOST")
	case errors.Is(err, objectstore.ErrMissing):
		gatewayError(w, 404, "ARTIFACT_MISSING")
	default:
		gatewayError(w, 503, "DEPENDENCY_UNAVAILABLE")
	}
}
