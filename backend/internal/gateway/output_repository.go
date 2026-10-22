package gateway

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
)

type SQLOutputs struct {
	DB     *pgxpool.Pool
	Bucket string
}

func outcomeError(value string) error {
	switch value {
	case "stale", "forbidden":
		return ErrCapability
	case "quota":
		return ErrUploadQuota
	case "busy", "conflict":
		return ErrUploadConflict
	default:
		return objectstore.ErrUnavailable
	}
}
func (s SQLOutputs) Begin(ctx context.Context, q UploadRequest) (UploadTicket, error) {
	var ticket UploadTicket
	if s.DB == nil || s.Bucket == "" || q.Certificate == nil {
		return ticket, objectstore.ErrInvalid
	}
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		ticket = UploadTicket{}
		if err := authorizeOutputIdentity(ctx, tx, q); err != nil {
			return err
		}
		var outcome string
		var upload, artifact *uuid.UUID
		var key, sha *string
		var err error
		if imageMetadataRequired(q.MediaType) {
			err = tx.QueryRow(ctx, `SELECT outcome,upload_id,artifact_id,object_key,sha256 FROM photo.begin_output_image($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, q.Identity.TaskID, q.Identity.Attempt, q.Identity.Fence, q.Identity.WorkerID, certificateHash(q.Certificate), q.IdempotencyKey, q.Role, q.Bytes, q.SHA256, q.MediaType, s.Bucket).Scan(&outcome, &upload, &artifact, &key, &sha)
		} else {
			err = tx.QueryRow(ctx, `SELECT outcome,upload_id,artifact_id,object_key,sha256 FROM photo.begin_output($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, q.Identity.TaskID, q.Identity.Attempt, q.Identity.Fence, q.Identity.WorkerID, certificateHash(q.Certificate), q.IdempotencyKey, q.Role, q.Bytes, q.SHA256, q.MediaType, s.Bucket).Scan(&outcome, &upload, &artifact, &key, &sha)
		}
		if err != nil {
			return err
		}
		if outcome != "created" && outcome != "replayed" {
			return outcomeError(outcome)
		}
		if upload == nil || artifact == nil || key == nil || sha == nil {
			return ErrUploadConflict
		}
		ticket = UploadTicket{ArtifactID: *artifact, UploadID: *upload, ObjectKey: *key}
		if outcome == "replayed" {
			ticket.Replay = &ArtifactRef{*artifact, *sha}
		}
		return nil
	})
	return ticket, err
}
func (s SQLOutputs) Complete(ctx context.Context, q UploadRequest, t UploadTicket, o objectstore.Object) (ArtifactRef, error) {
	var ref ArtifactRef
	if s.DB == nil || q.Certificate == nil {
		return ref, objectstore.ErrInvalid
	}
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		if err := authorizeOutputIdentity(ctx, tx, q); err != nil {
			return err
		}
		var outcome string
		var artifact *uuid.UUID
		var sha *string
		var err error
		if imageMetadataRequired(q.MediaType) {
			if q.intrinsicImage == nil {
				return objectstore.ErrIntegrity
			}
			err = tx.QueryRow(ctx, `SELECT outcome,artifact_id,sha256 FROM photo.complete_output_image($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, q.Identity.TaskID, q.Identity.Attempt, q.Identity.Fence, q.Identity.WorkerID, certificateHash(q.Certificate), t.UploadID, o.VersionID, o.Bytes, o.SHA256, o.ContentType, q.intrinsicImage.Width, q.intrinsicImage.Height).Scan(&outcome, &artifact, &sha)
		} else {
			if q.intrinsicImage != nil {
				return objectstore.ErrInvalid
			}
			err = tx.QueryRow(ctx, `SELECT outcome,artifact_id,sha256 FROM photo.complete_output($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, q.Identity.TaskID, q.Identity.Attempt, q.Identity.Fence, q.Identity.WorkerID, certificateHash(q.Certificate), t.UploadID, o.VersionID, o.Bytes, o.SHA256, o.ContentType).Scan(&outcome, &artifact, &sha)
		}
		if err != nil {
			return err
		}
		if outcome != "committed" && outcome != "replayed" {
			return outcomeError(outcome)
		}
		if artifact == nil || sha == nil {
			return ErrUploadConflict
		}
		ref = ArtifactRef{*artifact, *sha}
		return nil
	})
	return ref, err
}

func authorizeOutputIdentity(ctx context.Context, tx pgx.Tx, q UploadRequest) error {
	if !q.Identity.valid() || q.Certificate == nil || q.Certificate.IsCA || len(q.Certificate.Raw) == 0 {
		return ErrCapability
	}
	var tenant, run uuid.UUID
	err := tx.QueryRow(ctx, `SELECT tenant_id,run_id FROM photo.authorize_gateway_task($1,$2,$3,$4,$5)`, q.Identity.TaskID, q.Identity.Attempt, q.Identity.Fence, q.Identity.WorkerID, certificateHash(q.Certificate)).Scan(&tenant, &run)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCapability
	}
	if err != nil {
		return err
	}
	if tenant != q.Identity.TenantID || run != q.Identity.RunID {
		return ErrCapability
	}
	return nil
}
