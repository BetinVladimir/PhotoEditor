package upload

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2capture"
)

// DeviceBeginRequest binds the upload to exactly one open shoot session.
type DeviceBeginRequest struct {
	SessionID uuid.UUID `json:"session_id"`
	BeginRequest
}

func deviceSession(ctx context.Context, tx pgx.Tx, principal v2capture.DevicePrincipal, id uuid.UUID, lock bool) (Session, error) {
	if err := v2capture.AdmitDeviceTx(ctx, tx, principal); err != nil {
		return Session{}, err
	}
	out, err := load(ctx, tx, principal.TenantID, id, lock)
	if err != nil {
		return Session{}, err
	}
	if out.ProjectID != principal.ProjectID {
		return Session{}, domain.NewError("NOT_FOUND", 404)
	}
	var epoch int64
	var deviceID, shootID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT principal_id,capture_session_id,capture_credential_epoch
		FROM photo.upload_sessions WHERE tenant_id=$1 AND project_id=$2 AND id=$3
		AND principal_type='device'`, principal.TenantID, principal.ProjectID, id).Scan(&deviceID, &shootID, &epoch)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (deviceID != principal.DeviceID ||
		epoch != principal.CredentialEpoch || shootID == uuid.Nil)) {
		return Session{}, domain.NewError("NOT_FOUND", 404)
	}
	return out, err
}

// BeginDevice reserves ordinary upload quota. No object-store call occurs in
// the DB transaction; a retry can safely return the same initializing row.
func (s Service) BeginDevice(ctx context.Context, principal v2capture.DevicePrincipal,
	key string, raw DeviceBeginRequest) (Session, error) {
	request, mediaType, err := normalize(raw.BeginRequest)
	if err != nil || raw.SessionID == uuid.Nil || request.ProjectID != principal.ProjectID || request.CollectionID == nil {
		return Session{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if s.DB == nil || s.Objects == nil {
		return Session{}, errors.New("upload dependencies unavailable")
	}
	raw.BeginRequest = request
	var uploadID uuid.UUID
	err = database.TenantTransaction(ctx, s.DB, principal.TenantID, func(tx pgx.Tx) error {
		response, err := idempotency.Use(ctx, tx, idempotency.Scope{Tenant: principal.TenantID,
			Principal: principal.DeviceID, PrincipalType: "device", Route: "Upload.BeginDevice", Key: key},
			raw, s.now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
				if err := v2capture.AdmitDeviceTx(ctx, tx, principal); err != nil {
					return idempotency.Response{}, err
				}
				var valid bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.shoot_sessions s
				JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id
				WHERE s.tenant_id=$1 AND s.project_id=$2 AND s.id=$3 AND s.collection_id=$4
				AND s.state='open' AND s.capture_policy IS NOT NULL AND c.deleted_at IS NULL)`,
					principal.TenantID, principal.ProjectID, raw.SessionID, request.CollectionID).Scan(&valid); err != nil {
					return idempotency.Response{}, err
				}
				if !valid {
					return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
				}
				var storage, reserved, files, reservedFiles, byteLimit, fileLimit int64
				if err := tx.QueryRow(ctx, `SELECT u.storage_bytes,u.reserved_bytes,u.source_files,u.reserved_files,
				(t.quotas->>'storage_bytes')::bigint,(t.quotas->>'source_files')::bigint
				FROM photo.tenant_usage u JOIN photo.tenants t ON t.id=u.tenant_id
				WHERE u.tenant_id=$1 FOR UPDATE OF u`, principal.TenantID).Scan(
					&storage, &reserved, &files, &reservedFiles, &byteLimit, &fileLimit); err != nil {
					return idempotency.Response{}, err
				}
				if byteLimit < 1 || fileLimit < 1 || storage > math.MaxInt64-reserved ||
					storage+reserved > byteLimit-request.Bytes || files > math.MaxInt64-reservedFiles ||
					files+reservedFiles >= fileLimit {
					return idempotency.Response{}, domain.NewError("QUOTA_EXCEEDED", 422)
				}
				uploadID, reservationID := uuid.New(), uuid.New()
				objectKey, err := objectstore.StagingKey(principal.TenantID, uploadID)
				if err != nil {
					return idempotency.Response{}, err
				}
				expires := s.now().Add(24 * time.Hour)
				_, err = tx.Exec(ctx, `INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,
				principal_type,principal_id,expected_size,filename,assume_profile,object_key,state,
				expires_at,reservation_id,media_type,expected_sha256,capture_session_id,capture_credential_epoch)
				VALUES($1,$2,$3,$4,'device',$5,$6,$7,$8,$9,'initializing',$10,$11,$12,$13,$14,$15)`,
					uploadID, principal.TenantID, principal.ProjectID, request.CollectionID, principal.DeviceID,
					request.Bytes, request.Filename, request.AssumeProfile, objectKey, expires,
					reservationID, mediaType, request.SHA256, raw.SessionID, principal.CredentialEpoch)
				if err != nil {
					return idempotency.Response{}, err
				}
				_, err = tx.Exec(ctx, `INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,
				principal_id,holder_type,holder_id,bytes,files,state,expires_at)
				VALUES($1,$2,'device',$3,'upload',$4,$5,1,'reserved',$6)`,
					reservationID, principal.TenantID, principal.DeviceID, uploadID, request.Bytes, expires)
				if err != nil {
					return idempotency.Response{}, err
				}
				_, err = tx.Exec(ctx, `UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+$2,
				reserved_files=reserved_files+1 WHERE tenant_id=$1`, principal.TenantID, request.Bytes)
				if err != nil {
					return idempotency.Response{}, err
				}
				body, err := canonical.Marshal(map[string]string{"upload_id": uploadID.String()})
				return idempotency.Response{Status: 201, Body: body, ResourceID: &uploadID}, err
			})
		if err != nil {
			return err
		}
		if response.ResourceID == nil {
			return errors.New("upload idempotency record missing resource")
		}
		uploadID = *response.ResourceID
		_, err = deviceSession(ctx, tx, principal, uploadID, false)
		return err
	})
	if err != nil {
		return Session{}, err
	}
	return s.ensureMultipartDevice(ctx, principal, uploadID)
}

func (s Service) GetDevice(ctx context.Context, principal v2capture.DevicePrincipal, id uuid.UUID) (Session, error) {
	var out Session
	err := database.TenantTransaction(ctx, s.DB, principal.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = deviceSession(ctx, tx, principal, id, false)
		return err
	})
	return out, err
}

func (s Service) ensureMultipartDevice(ctx context.Context, principal v2capture.DevicePrincipal, id uuid.UUID) (Session, error) {
	current, err := s.GetDevice(ctx, principal, id)
	if err != nil || current.State != "initializing" {
		return current, err
	}
	multipartID, err := s.Objects.BeginMultipart(ctx, current.objectKey, current.mediaType)
	if err != nil {
		return Session{}, err
	}
	attached := false
	err = database.TenantTransaction(ctx, s.DB, principal.TenantID, func(tx pgx.Tx) error {
		locked, err := deviceSession(ctx, tx, principal, id, true)
		if err != nil {
			return err
		}
		if locked.State == "initializing" && locked.multipartID == nil && locked.ExpiresAt.After(s.now()) {
			_, err = tx.Exec(ctx, `UPDATE photo.upload_sessions SET multipart_id=$3,state='uploading',
				revision=revision+1 WHERE tenant_id=$1 AND id=$2`, principal.TenantID, id, multipartID)
			attached = err == nil
		}
		return err
	})
	if err != nil || !attached {
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		_ = s.Objects.AbortMultipart(abortCtx, current.objectKey, multipartID)
		cancel()
		if err != nil {
			return Session{}, err
		}
	}
	return s.GetDevice(ctx, principal, id)
}

func (s Service) SignDeviceParts(ctx context.Context, principal v2capture.DevicePrincipal,
	id uuid.UUID, request PartRequest) (PartURLs, error) {
	if len(request.PartNumbers) < 1 || len(request.PartNumbers) > 100 {
		return PartURLs{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	var out PartURLs
	err := database.TenantTransaction(ctx, s.DB, principal.TenantID, func(tx pgx.Tx) error {
		session, err := deviceSession(ctx, tx, principal, id, false)
		if err != nil {
			return err
		}
		if session.State != "uploading" || session.multipartID == nil || !session.ExpiresAt.After(s.now()) {
			return domain.NewError("UPLOAD_STATE_INVALID", 409)
		}
		if !validPartNumbers(request.PartNumbers, session) {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		// Presigning is local and has no object-store side effect. Keep the
		// admission row lock until URLs have been minted.
		items, err := s.Objects.SignUploadParts(ctx, session.objectKey, *session.multipartID,
			request.PartNumbers, 10*time.Minute)
		if errors.Is(err, objectstore.ErrInvalid) {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		out = PartURLs{UploadID: id, Items: items}
		return err
	})
	return out, err
}

func (s Service) CompleteDevice(ctx context.Context, principal v2capture.DevicePrincipal,
	id uuid.UUID, key string, request CompleteRequest) (Accepted, error) {
	var out Accepted
	err := database.TenantTransaction(ctx, s.DB, principal.TenantID, func(tx pgx.Tx) error {
		response, err := idempotency.Use(ctx, tx, idempotency.Scope{Tenant: principal.TenantID,
			Principal: principal.DeviceID, PrincipalType: "device", Route: "Upload.CompleteDevice/" + id.String(), Key: key},
			request, s.now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
				session, err := deviceSession(ctx, tx, principal, id, true)
				if err != nil {
					return idempotency.Response{}, err
				}
				if session.State != "uploading" || session.multipartID == nil || !session.ExpiresAt.After(s.now()) {
					return idempotency.Response{}, domain.NewError("UPLOAD_STATE_INVALID", 409)
				}
				if !validCompletion(request, session) {
					return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
				}
				for _, part := range request.Parts {
					_, err = tx.Exec(ctx, `INSERT INTO photo.upload_parts(tenant_id,upload_session_id,part_no,etag,state)
					VALUES($1,$2,$3,$4,'completed')`, principal.TenantID, id, part.PartNumber, part.ETag)
					if err != nil {
						return idempotency.Response{}, err
					}
				}
				_, err = tx.Exec(ctx, `UPDATE photo.upload_sessions SET state='completing',
				next_completion_at=clock_timestamp(),staging_candidate_id=$3,ingest_snapshot_id=$4,
				ingest_run_id=$5,revision=revision+1 WHERE tenant_id=$1 AND id=$2`,
					principal.TenantID, id, uuid.New(), uuid.New(), uuid.New())
				if err != nil {
					return idempotency.Response{}, err
				}
				body, err := canonical.Marshal(Accepted{JobID: id, Status: "queued"})
				return idempotency.Response{Status: 202, Body: body, ResourceID: &id}, err
			})
		if err != nil {
			return err
		}
		if response.ResourceID == nil || *response.ResourceID != id {
			return errors.New("upload completion idempotency mismatch")
		}
		if _, err = deviceSession(ctx, tx, principal, id, false); err != nil {
			return err
		}
		return json.Unmarshal(response.Body, &out)
	})
	return out, err
}
