// Package styleupload publishes untrusted Style packages, never photo ingests or
// accepted Styles. The ordinary style.import worker performs package validation.
package styleupload

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"path/filepath"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
	"strings"
	"time"
)

const MaxBytes = int64(100 << 20)

type Objects interface {
	BeginStylePackageMultipart(context.Context, uuid.UUID, uuid.UUID, string) (string, error)
	SignUploadParts(context.Context, string, string, []int, time.Duration) ([]objectstore.PartURL, error)
	CompleteMultipart(context.Context, string, string, string, int64, []objectstore.UploadedPart) (objectstore.PendingObject, error)
	RecoverCompletedMultipart(context.Context, string, string, int64) (objectstore.PendingObject, error)
	VerifyPending(context.Context, objectstore.PendingObject, string) (objectstore.Object, error)
}
type Service struct {
	DB      *pgxpool.Pool
	Objects Objects
	Bucket  string
}
type BeginRequest struct {
	Filename string `json:"filename"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
}
type Session struct {
	ID        uuid.UUID  `json:"upload_id"`
	Project   uuid.UUID  `json:"project_id"`
	State     string     `json:"state"`
	Filename  string     `json:"filename"`
	Bytes     int64      `json:"bytes"`
	SHA256    string     `json:"sha256"`
	PartBytes int64      `json:"part_bytes"`
	Parts     int        `json:"parts_total"`
	Expires   time.Time  `json:"expires_at"`
	Artifact  *uuid.UUID `json:"upload_artifact_id,omitempty"`
}
type CompleteRequest struct {
	Parts        []objectstore.UploadedPart `json:"parts"`
	ExpectedSize int64                      `json:"expected_size"`
}
type row struct {
	Session
	Actor, ArtifactID, Reservation uuid.UUID
	Key, Media                     string
	Multipart                      *string
	Started                        bool
	Token                          *uuid.UUID
	Until                          *time.Time
	CompleteSHA                    *string
}

func invalid() error { return domain.NewError("VALIDATION_FAILED", 422) }
func normalize(r BeginRequest) (BeginRequest, string, error) {
	if r.Filename == "" || r.Filename != strings.TrimSpace(r.Filename) || len([]byte(r.Filename)) > 255 || strings.ContainsAny(r.Filename, "/\\\x00\r\n") || r.Bytes < 2 || r.Bytes > MaxBytes {
		return r, "", invalid()
	}
	if _, e := domain.ParseDigest(r.SHA256); e != nil {
		return r, "", invalid()
	}
	media := ""
	switch strings.ToLower(filepath.Ext(r.Filename)) {
	case ".json":
		media = "application/json"
	case ".zip":
		media = "application/zip"
	default:
		return r, "", invalid()
	}
	return r, media, nil
}
func (s Service) scope(ctx context.Context, who auth.Identity, tid, pid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return errors.New("style package upload unavailable")
	}
	return tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		if _, e := project.Authorize(ctx, tx, tid, pid, who.UserID, role, domain.Editor, true); e != nil {
			return e
		}
		return fn(tx)
	})
}
func load(ctx context.Context, tx pgx.Tx, who auth.Identity, tid, pid, id uuid.UUID) (row, error) {
	var r row
	e := tx.QueryRow(ctx, `SELECT id,project_id,state,filename,bytes,sha256,expires_at,actor_id,artifact_id,reservation_id,object_key,media_type,multipart_id,create_started,io_token,io_until,complete_sha256 FROM photo.style_package_uploads WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tid, id).Scan(&r.ID, &r.Project, &r.State, &r.Filename, &r.Bytes, &r.SHA256, &r.Expires, &r.Actor, &r.ArtifactID, &r.Reservation, &r.Key, &r.Media, &r.Multipart, &r.Started, &r.Token, &r.Until, &r.CompleteSHA)
	if errors.Is(e, pgx.ErrNoRows) || e == nil && (r.Project != pid || r.Actor != who.UserID) {
		return row{}, domain.NewError("NOT_FOUND", 404)
	}
	r.PartBytes = objectstore.PartBytes
	r.Parts = int((r.Bytes + r.PartBytes - 1) / r.PartBytes)
	if r.State == "ready" {
		var ready bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.artifacts a JOIN photo.style_package_uploads u ON u.tenant_id=a.tenant_id AND u.artifact_id=a.id WHERE u.tenant_id=$1 AND u.id=$2 AND a.state='ready' AND a.object_version=u.object_version AND a.object_key=u.object_key AND a.sha256=u.sha256 AND a.bytes=u.bytes AND a.media_type=u.media_type AND a.verified_at IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()))`, tid, id).Scan(&ready); e != nil {
			return row{}, e
		}
		if !ready {
			return row{}, domain.NewError("UPLOAD_ARTIFACT_EXPIRED", 410)
		}
		a := r.ArtifactID
		r.Artifact = &a
	}
	return r, e
}
func (s Service) Get(ctx context.Context, who auth.Identity, tid, pid, id uuid.UUID) (Session, error) {
	var r row
	e := s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error { var e error; r, e = load(ctx, tx, who, tid, pid, id); return e })
	return r.Session, e
}
func (s Service) Begin(ctx context.Context, who auth.Identity, tid, pid uuid.UUID, key string, raw BeginRequest) (Session, error) {
	request, media, e := normalize(raw)
	if e != nil {
		return Session{}, e
	}
	var id uuid.UUID
	e = s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		response, e := idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "StylePackage.Begin:" + pid.String(), Key: key}, request, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			var used, reserved, limit int64
			if e := tx.QueryRow(ctx, `SELECT u.storage_bytes,u.reserved_bytes,(t.quotas->>'storage_bytes')::bigint FROM photo.tenant_usage u JOIN photo.tenants t ON t.id=u.tenant_id WHERE u.tenant_id=$1 FOR UPDATE OF u`, tid).Scan(&used, &reserved, &limit); e != nil {
				return idempotency.Response{}, e
			}
			if used < 0 || reserved < 0 || limit < request.Bytes || used > limit-request.Bytes || reserved > limit-request.Bytes-used {
				return idempotency.Response{}, domain.NewError("QUOTA_EXCEEDED", 422)
			}
			id, reservation, artifact := uuid.New(), uuid.New(), uuid.New()
			objectKey, e := objectstore.StylePackageKey(tid, id)
			if e != nil {
				return idempotency.Response{}, e
			}
			expires := time.Now().UTC().Add(24 * time.Hour)
			if _, e = tx.Exec(ctx, `INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES($1,$2,'user',$3,'style_package_upload',$4,$5,0,'reserved',$6)`, reservation, tid, who.UserID, id, request.Bytes, expires); e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.style_package_uploads(tenant_id,id,project_id,actor_id,artifact_id,reservation_id,filename,bytes,sha256,media_type,object_key,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tid, id, pid, who.UserID, artifact, reservation, request.Filename, request.Bytes, request.SHA256, media, objectKey, expires); e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+$2 WHERE tenant_id=$1`, tid, request.Bytes); e != nil {
				return idempotency.Response{}, e
			}
			body, e := canonical.Marshal(map[string]uuid.UUID{"upload_id": id})
			return idempotency.Response{Status: 201, Body: body, ResourceID: &id}, e
		})
		if e != nil {
			return e
		}
		if response.ResourceID == nil {
			return errors.New("missing package reservation")
		}
		id = *response.ResourceID
		_, e = load(ctx, tx, who, tid, pid, id)
		return e
	})
	if e != nil {
		return Session{}, e
	}
	return s.initialize(ctx, who, tid, pid, id)
}
func (s Service) initialize(ctx context.Context, who auth.Identity, tid, pid, id uuid.UUID) (Session, error) {
	var r row
	token := uuid.New()
	create := false
	e := s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		var e error
		r, e = load(ctx, tx, who, tid, pid, id)
		if e != nil {
			return e
		}
		if r.State == "uploading" || r.State == "ready" {
			return nil
		}
		if r.State != "reserved" || r.Started || !r.Expires.After(time.Now()) {
			return domain.NewError("UPLOAD_INITIALIZATION_PENDING", 409)
		}
		_, e = tx.Exec(ctx, `UPDATE photo.style_package_uploads SET state='initializing',create_started=true,io_token=$3,io_until=clock_timestamp()+interval '90 seconds' WHERE tenant_id=$1 AND id=$2`, tid, id, token)
		create = e == nil
		return e
	})
	if e != nil || !create {
		return r.Session, e
	}
	ioCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	multipart, e := s.Objects.BeginStylePackageMultipart(ioCtx, tid, id, r.Media)
	if e != nil {
		if errors.Is(e, objectstore.ErrStylePackageCreateNotCreated) {
			// Closing a proven absent IO attempt is safe even after access withdrawal.
			// Private token helper grants no new upload/object authority.
			closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer closeCancel()
			var closed bool
			closeErr := s.DB.QueryRow(closeCtx, "SELECT photo.close_style_package_create_without_object($1,$2,$3)", tid, id, token).Scan(&closed)
			if closeErr != nil || !closed {
				return Session{}, errors.Join(e, closeErr, errors.New("package create outcome not durably closed"))
			}
		}
		return Session{}, e
	}
	if !objectstore.ValidInterchangeMultipartID(multipart) {
		return Session{}, errors.New("invalid package multipart identity")
	}
	e = s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		current, e := load(ctx, tx, who, tid, pid, id)
		if e != nil {
			return e
		}
		if current.State != "initializing" || current.Token == nil || *current.Token != token || current.Until == nil || !current.Until.After(time.Now()) || !current.Expires.After(time.Now()) {
			return domain.NewError("UPLOAD_STALE", 409)
		}
		_, e = tx.Exec(ctx, `UPDATE photo.style_package_uploads SET state='uploading',multipart_id=$3,io_token=NULL,io_until=NULL WHERE tenant_id=$1 AND id=$2`, tid, id, multipart)
		return e
	})
	if e != nil {
		return Session{}, e
	}
	return s.Get(ctx, who, tid, pid, id)
}
func (s Service) Sign(ctx context.Context, who auth.Identity, tid, pid, id uuid.UUID, numbers []int) ([]objectstore.PartURL, error) {
	var r row
	until := time.Now().UTC().Add(11 * time.Minute)
	seen := map[int]bool{}
	e := s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		var e error
		r, e = load(ctx, tx, who, tid, pid, id)
		if e != nil {
			return e
		}
		if r.State != "uploading" || r.Multipart == nil || !r.Expires.After(until) {
			return domain.NewError("UPLOAD_STALE", 409)
		}
		if len(numbers) < 1 || len(numbers) > r.Parts {
			return invalid()
		}
		for _, n := range numbers {
			if n < 1 || n > r.Parts || seen[n] {
				return invalid()
			}
			seen[n] = true
		}
		_, e = tx.Exec(ctx, `UPDATE photo.style_package_uploads SET signed_until=greatest(signed_until,$3) WHERE tenant_id=$1 AND id=$2`, tid, id, until)
		return e
	})
	if e != nil {
		return nil, e
	}
	urls, e := s.Objects.SignUploadParts(ctx, r.Key, *r.Multipart, numbers, 10*time.Minute)
	if e != nil {
		return nil, e
	}
	if len(urls) != len(numbers) {
		return nil, errors.New("signed package part inventory mismatch")
	}
	received := map[int]bool{}
	for _, u := range urls {
		if !seen[u.PartNumber] || received[u.PartNumber] || !u.ExpiresAt.Before(until) || !u.ExpiresAt.After(time.Now()) {
			return nil, errors.New("signed package part scope mismatch")
		}
		received[u.PartNumber] = true
	}
	e = s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		current, e := load(ctx, tx, who, tid, pid, id)
		if e != nil {
			return e
		}
		if current.State != "uploading" || current.Multipart == nil || *current.Multipart != *r.Multipart {
			return domain.NewError("UPLOAD_STALE", 409)
		}
		return nil
	})
	return urls, e
}
func validateComplete(r row, request CompleteRequest) error {
	if request.ExpectedSize != r.Bytes || len(request.Parts) != r.Parts {
		return invalid()
	}
	for i, p := range request.Parts {
		if p.PartNumber != i+1 || len(p.ETag) < 1 || len(p.ETag) > 256 || strings.ContainsAny(p.ETag, "\r\n\x00") {
			return invalid()
		}
	}
	return nil
}
func (s Service) Complete(ctx context.Context, who auth.Identity, tid, pid, id uuid.UUID, key string, request CompleteRequest) (Session, error) {
	if !idempotency.ValidKey(key) {
		return Session{}, invalid()
	}
	completeSHA, e := canonical.Hash(struct {
		Key  string          `json:"key"`
		Body CompleteRequest `json:"body"`
	}{key, request})
	if e != nil {
		return Session{}, e
	}
	var r row
	token := uuid.New()
	ready := false
	e = s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		var e error
		r, e = load(ctx, tx, who, tid, pid, id)
		if e != nil {
			return e
		}
		if e = validateComplete(r, request); e != nil {
			return e
		}
		if r.CompleteSHA != nil && *r.CompleteSHA != completeSHA {
			return domain.NewError("IDEMPOTENCY_CONFLICT", 409)
		}
		if r.State == "ready" {
			ready = true
			return nil
		}
		if r.State != "uploading" && r.State != "verifying" || r.Multipart == nil || !r.Expires.After(time.Now()) || r.Until != nil && r.Until.After(time.Now()) {
			return domain.NewError("UPLOAD_BUSY_OR_STALE", 409)
		}
		_, e = tx.Exec(ctx, `UPDATE photo.style_package_uploads SET state='verifying',io_token=$3,io_until=clock_timestamp()+interval '150 seconds',complete_sha256=$4 WHERE tenant_id=$1 AND id=$2`, tid, id, token, completeSHA)
		return e
	})
	if e != nil || ready {
		return r.Session, e
	}
	ioCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	pending, e := s.Objects.RecoverCompletedMultipart(ioCtx, r.Key, r.Media, r.Bytes)
	if errors.Is(e, objectstore.ErrMissing) {
		pending, e = s.Objects.CompleteMultipart(ioCtx, r.Key, *r.Multipart, r.Media, r.Bytes, request.Parts)
	}
	if e != nil {
		return Session{}, e
	}
	object, e := s.Objects.VerifyPending(ioCtx, pending, r.SHA256)
	if e != nil {
		return Session{}, e
	}
	if object.Key != r.Key || object.Bytes != r.Bytes || object.SHA256 != r.SHA256 || object.ContentType != r.Media || !objectstore.ValidInterchangeVersionID(object.VersionID) {
		return Session{}, errors.New("verified package identity mismatch")
	}
	e = s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		// Match reservation lock order used by Begin/GC: usage before publication row.
		var reserved int64
		if e := tx.QueryRow(ctx, `SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1 FOR UPDATE`, tid).Scan(&reserved); e != nil {
			return e
		}
		current, e := load(ctx, tx, who, tid, pid, id)
		if e != nil {
			return e
		}
		if current.State != "verifying" || current.Token == nil || *current.Token != token || current.Until == nil || !current.Until.After(time.Now()) || !current.Expires.After(time.Now()) {
			return domain.NewError("UPLOAD_STALE", 409)
		}
		var amount int64
		var state string
		if e = tx.QueryRow(ctx, `SELECT bytes,state FROM photo.quota_reservations WHERE tenant_id=$1 AND id=$2 AND holder_type='style_package_upload' AND holder_id=$3 FOR UPDATE`, tid, r.Reservation, id).Scan(&amount, &state); e != nil {
			return e
		}
		if amount != r.Bytes || state != "reserved" || reserved < amount {
			return errors.New("package quota reservation mismatch")
		}
		if _, e = tx.Exec(ctx, `INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'ready',clock_timestamp())`, r.ArtifactID, tid, s.Bucket, object.Key, object.VersionID, object.SHA256, object.Bytes, object.ContentType); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE photo.quota_reservations SET state='consumed' WHERE tenant_id=$1 AND id=$2`, tid, r.Reservation); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes-$2,storage_bytes=storage_bytes+$2 WHERE tenant_id=$1`, tid, amount); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE photo.style_package_uploads SET state='ready',object_version=$3,io_token=NULL,io_until=NULL WHERE tenant_id=$1 AND id=$2`, tid, id, object.VersionID)
		return e
	})
	if e != nil {
		return Session{}, e
	}
	return s.Get(ctx, who, tid, pid, id)
}
func (s Service) Abort(ctx context.Context, who auth.Identity, tid, pid, id uuid.UUID) (Session, error) {
	e := s.scope(ctx, who, tid, pid, func(tx pgx.Tx) error {
		r, e := load(ctx, tx, who, tid, pid, id)
		if e != nil {
			return e
		}
		if r.State == "ready" {
			return domain.NewError("UPLOAD_ALREADY_PUBLISHED", 409)
		}
		if r.State == "aborted" || r.State == "aborting" || r.State == "purging" {
			return nil
		}
		_, e = tx.Exec(ctx, `UPDATE photo.style_package_uploads SET state='aborting' WHERE tenant_id=$1 AND id=$2`, tid, id)
		return e
	})
	if e != nil {
		return Session{}, e
	}
	return s.Get(ctx, who, tid, pid, id)
}

// The production object store must satisfy this publication contract at build time.
var _ Objects = (*objectstore.Store)(nil)
