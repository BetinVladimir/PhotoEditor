package gateway

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"net/http"
	"photoeditor/backend/internal/objectstore"
	"regexp"
	"time"
)

var ErrUploadConflict = errors.New("output upload conflict")
var ErrUploadQuota = errors.New("output upload quota exceeded")
var uploadRole = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var uploadHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var uploadMedia = regexp.MustCompile(`^[a-z0-9.+-]+/[a-z0-9.+-]+$`)

type ArtifactRef struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}
type UploadRequest struct {
	Identity                TaskIdentity
	Certificate             *x509.Certificate
	IdempotencyKey          uuid.UUID
	Role, MediaType, SHA256 string
	Bytes                   int64
}
type UploadTicket struct {
	ArtifactID uuid.UUID
	UploadID   uuid.UUID
	ObjectKey  string
	Replay     *ArtifactRef
}

// Begin reserves quota and role count under the canonical task locks and checks
// live lease/certificate. Reusing a key with different metadata must conflict;
// a pending concurrent writer must not receive the same ticket for execution.
// Complete rechecks lease/fence under locks and persists verified artifact,
// consumes reserved bytes exactly once, and returns only a canonical reference.
// Unknown storage/DB outcomes retain their staging record for reconciliation;
// they must not release quota or remove a possibly committed object speculatively.
type OutputRepository interface {
	Begin(context.Context, UploadRequest) (UploadTicket, error)
	Complete(context.Context, UploadRequest, UploadTicket, objectstore.Object) (ArtifactRef, error)
}
type ObjectWriter interface {
	PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error)
}
type UploadService struct {
	Repository OutputRepository
	Objects    ObjectWriter
}

func (s *UploadService) Upload(ctx context.Context, request UploadRequest, body io.Reader) (ArtifactRef, bool, error) {
	if s == nil || s.Repository == nil || s.Objects == nil {
		return ArtifactRef{}, false, objectstore.ErrUnavailable
	}
	if !request.Identity.valid() || request.IdempotencyKey == uuid.Nil || request.Certificate == nil || request.Certificate.IsCA || len(request.Certificate.Raw) == 0 || !uploadRole.MatchString(request.Role) || !uploadHash.MatchString(request.SHA256) || !uploadMedia.MatchString(request.MediaType) || request.Bytes < 0 || body == nil {
		return ArtifactRef{}, false, objectstore.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, false, err
	}
	beginCtx, endBegin := context.WithTimeout(ctx, 5*time.Second)
	ticket, err := s.Repository.Begin(beginCtx, request)
	beginContextErr := beginCtx.Err()
	endBegin()
	if err != nil {
		dependencyObservation("output_reservation", err)
		return ArtifactRef{}, false, err
	}
	if beginContextErr != nil {
		return ArtifactRef{}, false, beginContextErr
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, false, err
	}
	if ticket.Replay != nil {
		if ticket.Replay.ArtifactID == uuid.Nil || ticket.Replay.SHA256 != request.SHA256 {
			return ArtifactRef{}, false, ErrUploadConflict
		}
		return *ticket.Replay, true, nil
	}
	key, err := objectstore.AttemptKey(request.Identity.TenantID, request.Identity.TaskID, ticket.ArtifactID, request.Identity.Attempt, request.Identity.Fence)
	if err != nil || ticket.UploadID == uuid.Nil || ticket.ObjectKey != key {
		return ArtifactRef{}, false, ErrUploadConflict
	}
	object, err := s.Objects.PutImmutable(ctx, key, body, request.Bytes, request.SHA256, request.MediaType)
	if err != nil {
		dependencyObservation("output_storage", err)
		return ArtifactRef{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, false, err
	}
	if object.Key != key || object.VersionID == "" || object.VersionID == "null" || object.Bytes != request.Bytes || object.SHA256 != request.SHA256 || object.ContentType != request.MediaType {
		return ArtifactRef{}, false, objectstore.ErrIntegrity
	}
	commitCtx, endCommit := context.WithTimeout(ctx, 5*time.Second)
	ref, err := s.Repository.Complete(commitCtx, request, ticket, object)
	commitContextErr := commitCtx.Err()
	endCommit()
	if err != nil {
		dependencyObservation("output_commit", err)
		return ArtifactRef{}, false, err
	}
	if commitContextErr != nil {
		return ArtifactRef{}, false, commitContextErr
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, false, err
	}
	if ref.ArtifactID != ticket.ArtifactID || ref.SHA256 != request.SHA256 {
		return ArtifactRef{}, false, ErrUploadConflict
	}
	return ref, false, nil
}

func (h *HTTP) upload(w http.ResponseWriter, r *http.Request) {
	task, err := h.AuthorizeRequest(r, WriteOutputs)
	if err != nil {
		downloadError(w, err)
		return
	}
	if h.Uploads == nil {
		gatewayError(w, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	if r.ContentLength < 0 || len(r.TransferEncoding) != 0 {
		gatewayError(w, 411, "CONTENT_LENGTH_REQUIRED")
		return
	}
	if h.MaxUploadBytes <= 0 || r.ContentLength > h.MaxUploadBytes {
		gatewayError(w, 413, "DISK_QUOTA")
		return
	}
	role := r.PathValue("role")
	sha, shaErr := singleHeader(r, "X-Photo-SHA256")
	media, mediaErr := singleHeader(r, "Content-Type")
	rawKey, keyErr := singleHeader(r, "Idempotency-Key")
	key, idErr := canonicalUUID(rawKey)
	if shaErr != nil || mediaErr != nil || keyErr != nil || idErr != nil || !uploadRole.MatchString(role) || !uploadHash.MatchString(sha) || !uploadMedia.MatchString(media) {
		gatewayError(w, 400, "VALIDATION_FAILED")
		return
	}
	cert, err := VerifiedPeer(r.TLS)
	if err != nil {
		downloadError(w, err)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), task.Deadline)
	defer cancel()
	controller := http.NewResponseController(w)
	defer controller.SetReadDeadline(time.Time{})
	request := UploadRequest{Identity: task.Identity, Certificate: cert, IdempotencyKey: key, Role: role, MediaType: media, SHA256: sha, Bytes: r.ContentLength}
	body := &uploadReader{reader: http.MaxBytesReader(w, r.Body, h.MaxUploadBytes), controller: controller, ctx: ctx, h: h, task: task, cert: cert}
	ref, replay, err := h.Uploads.Upload(ctx, request, body)
	if err != nil && body.failure != nil {
		err = body.failure
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrUploadQuota):
			gatewayError(w, 413, "DISK_QUOTA")
		case errors.Is(err, ErrUploadConflict), errors.Is(err, objectstore.ErrExists):
			gatewayError(w, 409, "UPLOAD_CONFLICT")
		case errors.Is(err, objectstore.ErrIntegrity), errors.Is(err, objectstore.ErrInvalid):
			gatewayError(w, 422, "ARTIFACT_HASH_MISMATCH")
		default:
			downloadError(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if replay {
		w.WriteHeader(200)
	} else {
		w.WriteHeader(201)
	}
	_ = json.NewEncoder(w).Encode(ref)
}

type uploadReader struct {
	reader     io.Reader
	controller *http.ResponseController
	ctx        context.Context
	h          *HTTP
	task       TaskContext
	cert       *x509.Certificate
	next       time.Time
	failure    error
}

func (r *uploadReader) Read(p []byte) (n int, err error) {
	defer func() {
		if err != nil && err != io.EOF {
			r.failure = err
		}
	}()
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if !time.Now().Before(r.next) {
		ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
		current, err := r.h.Tasks.Resolve(ctx, r.task.Identity.TaskID, r.task.Identity.Attempt, r.task.Identity.Fence, r.task.Identity.WorkerID, r.cert)
		cancel()
		if err != nil {
			return 0, err
		}
		if current.Identity != r.task.Identity {
			return 0, ErrCapability
		}
		r.next = time.Now().Add(5 * time.Second)
	}
	if len(p) > 64*1024 {
		p = p[:64*1024]
	}
	_ = r.controller.SetReadDeadline(time.Now().Add(15 * time.Second))
	return r.reader.Read(p)
}
