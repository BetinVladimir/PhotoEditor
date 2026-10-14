package provenance

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Dedicated SQL scope binds ordinary task_params to the saved signing draft;
// generic backend document publication cannot manufacture this authority.
type SQLSigningParamsRepository struct {
	DB       *pgxpool.Pool
	Who      auth.Identity
	Ready    ReadySigningExport
	Prepared PreparedSigningActivation
	Bucket   string
}

func (s SQLSigningParamsRepository) validate(scope artifactjson.Scope, sha string, size int64) (v2input.Ref, error) {
	if s.DB == nil {
		return v2input.Ref{}, ErrInvalid
	}
	if _, err := compileSigningPending(s.Who, s.Ready, s.Prepared, s.Bucket); err != nil {
		return v2input.Ref{}, err
	}
	i := s.Prepared.intent
	expected, body := s.Prepared.compiled.ParamsDocument()
	if scope != (artifactjson.Scope{Tenant: i.tenant, Run: i.run, Owner: i.task, Purpose: "task_params"}) || sha != expected.SHA256 || size != int64(len(body)) || size < 2 || size > 65536 {
		return v2input.Ref{}, ErrInvalid
	}
	return expected, nil
}
func (s SQLSigningParamsRepository) Reserve(ctx context.Context, scope artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	var result artifactjson.Reservation
	if err := ctx.Err(); err != nil {
		return result, err
	}
	expected, err := s.validate(scope, sha, size)
	if err != nil {
		return result, err
	}
	i := s.Prepared.intent
	err = tenant.WithTenant(ctx, s.DB, s.Who, i.tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outcome string
		var id, token *uuid.UUID
		var ready bool
		if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v31_signing_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, i.tenant, s.Who.UserID, s.Who.SessionID, i.task, i.key, i.requestSHA, expected.ArtifactID, sha, size, s.Bucket).Scan(&outcome, &id, &token, &ready); err != nil {
			return err
		}
		var err error
		result, err = signingLineageReservation(outcome, id, token, ready, expected)
		return err
	})
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	return result, nil
}
func (s SQLSigningParamsRepository) Complete(ctx context.Context, scope artifactjson.Scope, reservation artifactjson.Reservation, object objectstore.Object) (artifactjson.Ref, error) {
	var zero artifactjson.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	expected, err := s.validate(scope, object.SHA256, object.Bytes)
	if err != nil {
		return zero, err
	}
	key, err := objectstore.DocumentKey(scope.Tenant, scope.Run, expected.ArtifactID)
	if err != nil || reservation.ArtifactID != expected.ArtifactID || reservation.Token == uuid.Nil || reservation.Ready != nil || object.Key != key || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return zero, objectstore.ErrIntegrity
	}
	i := s.Prepared.intent
	err = tenant.WithTenant(ctx, s.DB, s.Who, i.tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v31_signing_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, i.tenant, s.Who.UserID, s.Who.SessionID, i.task, i.key, i.requestSHA, expected.ArtifactID, expected.SHA256, object.Bytes, s.Bucket, reservation.Token, object.VersionID).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return artifactjson.Ref{ID: expected.ArtifactID, SHA256: expected.SHA256}, nil
}
func (s SQLSigningParamsRepository) Publish(ctx context.Context, objects artifactjson.Objects) (v2input.Ref, error) {
	var zero v2input.Ref
	if objects == nil {
		return zero, ErrInvalid
	}
	i := s.Prepared.intent
	expected, body := s.Prepared.compiled.ParamsDocument()
	scope := artifactjson.Scope{Tenant: i.tenant, Run: i.run, Owner: i.task, Purpose: "task_params"}
	if _, err := s.validate(scope, expected.SHA256, int64(len(body))); err != nil {
		return zero, err
	}
	receipt, err := (artifactjson.Publisher{Repository: s, Objects: objects}).Publish(ctx, scope, body)
	if err != nil {
		return zero, err
	}
	if receipt.ID != expected.ArtifactID || receipt.SHA256 != expected.SHA256 {
		return zero, objectstore.ErrIntegrity
	}
	return expected, nil
}

var _ artifactjson.Repository = SQLSigningParamsRepository{}
