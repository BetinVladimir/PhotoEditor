package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type SQLDonorConfirmationRepository struct {
	DB     DonorConfirmationTransactions
	Bucket string
}

func (s SQLDonorConfirmationRepository) begin(ctx context.Context, request DonorConfirmationPublication) (pgx.Tx, []byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s.DB == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return nil, nil, v2input.ErrInvalid
	}
	body, err := DonorIssuerContext(request)
	if err != nil {
		return nil, nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	if tx == nil {
		return nil, nil, v2input.ErrInvalid
	}
	fail := func(err error) (pgx.Tx, []byte, error) { tx.Rollback(context.WithoutCancel(ctx)); return nil, nil, err }
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", request.Tenant.String()); err != nil {
		return fail(err)
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return fail(err)
	}
	if role != "photo_scheduler" {
		return fail(v2input.ErrInvalid)
	}
	return tx, body, nil
}

func (s SQLDonorConfirmationRepository) ReserveDonorConfirmation(ctx context.Context, request DonorConfirmationPublication, size int64) (DonorConfirmationReservation, error) {
	var zero DonorConfirmationReservation
	if size != int64(len(request.Prepared.Body)) || size < 2 || size > 65536 {
		return zero, v2input.ErrInvalid
	}
	tx, body, err := s.begin(ctx, request)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = tx.QueryRow(ctx, "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_donor_document($1,$2,$3,$4::jsonb,$5,$6,$7)", request.Tenant, request.RunID, request.Prepared.Params.Confirmation.ArtifactID, string(body), request.Prepared.SHA256, size, s.Bucket).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return zero, err
	}
	switch outcome {
	case "conflict":
		return zero, artifactjson.ErrConflict
	case "stale", "forbidden":
		return zero, artifactjson.ErrStale
	case "quota":
		return zero, artifactjson.ErrQuota
	case "created", "resumed", "replayed":
	default:
		return zero, v2input.ErrInvalid
	}
	if id == nil || token == nil || *id == uuid.Nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return zero, v2input.ErrInvalid
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	result := DonorConfirmationReservation{RunID: request.RunID, ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &v2input.Ref{ArtifactID: *id, SHA256: request.Prepared.SHA256}
	}
	return result, nil
}

func (s SQLDonorConfirmationRepository) CompleteDonorConfirmation(ctx context.Context, request DonorConfirmationPublication, reservation DonorConfirmationReservation, object objectstore.Object) (v2input.Ref, error) {
	var zero v2input.Ref
	key, err := objectstore.DocumentKey(request.Tenant, request.RunID, reservation.ArtifactID)
	if err != nil {
		return zero, err
	}
	if reservation.RunID != request.RunID || reservation.Token == uuid.Nil || object.Key != key || object.SHA256 != request.Prepared.SHA256 || object.Bytes != int64(len(request.Prepared.Body)) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return zero, v2input.ErrInvalid
	}
	tx, body, err := s.begin(ctx, request)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.complete_v28_donor_document($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10)", request.Tenant, request.RunID, request.Prepared.Params.Confirmation.ArtifactID, string(body), reservation.ArtifactID, reservation.Token, request.Prepared.SHA256, object.Bytes, s.Bucket, object.VersionID).Scan(&ready)
	if err != nil {
		return zero, err
	}
	if !ready {
		return zero, artifactjson.ErrStale
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return v2input.Ref{ArtifactID: reservation.ArtifactID, SHA256: request.Prepared.SHA256}, nil
}
