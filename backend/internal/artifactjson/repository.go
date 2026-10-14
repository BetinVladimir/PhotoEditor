package artifactjson

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

var ErrConflict = errors.New("document reservation conflict")
var ErrStale = errors.New("document reservation stale")
var ErrQuota = errors.New("document storage quota exceeded")

type RowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// SQLRepository uses a photo_scheduler connection. Each function call commits
// independently; no caller transaction may span Publisher object IO.
type SQLRepository struct {
	DB     RowQuerier
	Bucket string
}

func (s SQLRepository) Reserve(ctx context.Context, scope Scope, sha string, size int64) (Reservation, error) {
	if s.DB == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || size < 2 || size > int64(maxScopeDocumentBytes(scope)) {
		return Reservation{}, objectstore.ErrInvalid
	}
	if _, err := DocumentKey(scope, scope.Owner); err != nil {
		return Reservation{}, err
	}
	if _, err := domain.ParseDigest(sha); err != nil {
		return Reservation{}, objectstore.ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	query := "SELECT outcome,artifact_id,token,ready FROM photo.reserve_backend_document($1,$2,$3,$4,$5,$6,$7)"
	purposeArg := scope.Purpose
	args := []any{scope.Tenant, scope.Run, scope.Owner, purposeArg, sha, size, s.Bucket}
	if scope.DonorPortrait {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_donor_params_document($1,$2,$3,$4,$5,$6,$7)"
	} else if scope.LocalPortrait {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_local_params_document($1,$2,$3,$4,$5,$6,$7)"
	} else if scope.RestorationPortrait {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v28_restoration_params_document($1,$2,$3,$4,$5,$6,$7)"
	}
	if v2InputPurpose(scope.Purpose) {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_input_document($1,$2,$3,$4,$5,$6,$7)"
	} else if v25LensPurpose(scope.Purpose) {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v25_lens_document($1,$2,$3,$4,$5,$6,$7)"
	} else if v24CapturePurpose(scope.Purpose) {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v24_capture_document($1,$2,$3,$4,$5,$6,$7)"
	} else if scope.Purpose == "v2_session_plan_params" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v22_session_plan_document($1,$2,$3,$4,$5,$6,$7)"
	} else if scope.Purpose == "v23_evaluate_params" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v23_evaluate_document($1,$2,$3,$4,$5,$6,$7)"
	} else if v22AnalysisPurpose(scope.Purpose) {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v22_analysis_document($1,$2,$3,$4,$5,$6,$7)"
	} else if scope.Purpose == "v23_training_manifest" || scope.Purpose == "v23_training_snapshot" || scope.Purpose == "v23_fit_params" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v23_training_document($1,$2,$3,$4,$5,$6,$7)"
	} else if scope.Purpose == "v23_frozen_base" || scope.Purpose == "v23_frozen_approved" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v23_training_frozen($1,$2,$3,$4,$5,$6,$7)"
		purposeArg = scope.Purpose[len("v23_frozen_"):]
	} else if scope.Purpose == "v23_defect_holdout" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v23_reviewed_defect_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.Review,
			scope.Actor, scope.AuthSession, scope.DecisionHash, sha, size, s.Bucket}
	} else if scope.Purpose == "v23_dust_holdout" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v23_reviewed_dust_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.Review,
			scope.Actor, scope.AuthSession, scope.DecisionHash, sha, size, s.Bucket}
	} else if scope.Purpose == "v23_detail_holdout" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v23_detail_document($1,$2,$3,$4,$5,$6,$7,$8,$9)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.Project,
			scope.Actor, scope.AuthSession, sha, size, s.Bucket}
	} else if scope.Purpose == "v23_zero_profile" {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v23_training_zero($1,$2,$3,$4,$5,$6)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, sha, size, s.Bucket}
	}
	if scope.Purpose != "v23_zero_profile" && scope.Purpose != "v23_defect_holdout" && scope.Purpose != "v23_dust_holdout" && scope.Purpose != "v23_detail_holdout" {
		args[3] = purposeArg
	}
	if scope.AssemblyLease != uuid.Nil {
		query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v27_semantic_assembly_document($1,$2,$3,$4,$5,$6,$7,$8)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.AssemblyLease, scope.Purpose, sha, size, s.Bucket}
		if v2InputPurpose(scope.Purpose) {
			query = "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v27_semantic_transport_document($1,$2,$3,$4,$5,$6,$7,$8)"
		}
	}
	err := s.DB.QueryRow(ctx, query, args...).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return Reservation{}, err
	}
	switch outcome {
	case "conflict":
		return Reservation{}, ErrConflict
	case "stale", "forbidden":
		return Reservation{}, ErrStale
	case "quota":
		return Reservation{}, ErrQuota
	case "created", "resumed", "replayed":
	default:
		return Reservation{}, objectstore.ErrIntegrity
	}
	if id == nil || *id == uuid.Nil || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
		return Reservation{}, objectstore.ErrIntegrity
	}
	result := Reservation{ArtifactID: *id, Token: *token}
	if ready {
		result.Ready = &Ref{*id, sha}
	}
	return result, nil
}
func (s SQLRepository) Complete(ctx context.Context, scope Scope, ticket Reservation, object objectstore.Object) (Ref, error) {
	if s.DB == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || ticket.Token == uuid.Nil || object.Bytes < 2 || object.Bytes > int64(maxScopeDocumentBytes(scope)) || object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 {
		return Ref{}, objectstore.ErrInvalid
	}
	key, err := DocumentKey(scope, ticket.ArtifactID)
	if err != nil {
		return Ref{}, err
	}
	if object.Key != key {
		return Ref{}, objectstore.ErrIntegrity
	}
	if _, err = domain.ParseDigest(object.SHA256); err != nil {
		return Ref{}, objectstore.ErrInvalid
	}
	var done bool
	query := "SELECT photo.complete_backend_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	purposeArg := scope.Purpose
	args := []any{scope.Tenant, scope.Run, scope.Owner, purposeArg,
		ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes,
		s.Bucket, object.VersionID}
	if scope.DonorPortrait {
		query = "SELECT photo.complete_v28_donor_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if scope.LocalPortrait {
		query = "SELECT photo.complete_v28_local_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if scope.RestorationPortrait {
		query = "SELECT photo.complete_v28_restoration_params_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	}
	if v2InputPurpose(scope.Purpose) {
		query = "SELECT photo.complete_v2_input_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if v25LensPurpose(scope.Purpose) {
		query = "SELECT photo.complete_v25_lens_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if v24CapturePurpose(scope.Purpose) {
		query = "SELECT photo.complete_v24_capture_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if scope.Purpose == "v2_session_plan_params" {
		query = "SELECT photo.complete_v22_session_plan_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if scope.Purpose == "v23_evaluate_params" {
		query = "SELECT photo.complete_v23_evaluate_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if v22AnalysisPurpose(scope.Purpose) {
		query = "SELECT photo.complete_v22_analysis_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if scope.Purpose == "v23_training_manifest" || scope.Purpose == "v23_training_snapshot" || scope.Purpose == "v23_fit_params" {
		query = "SELECT photo.complete_v23_training_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
	} else if scope.Purpose == "v23_frozen_base" || scope.Purpose == "v23_frozen_approved" {
		query = "SELECT photo.complete_v23_training_frozen($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"
		purposeArg = scope.Purpose[len("v23_frozen_"):]
	} else if scope.Purpose == "v23_defect_holdout" {
		query = "SELECT photo.complete_v23_reviewed_defect_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.Review,
			scope.Actor, scope.AuthSession, scope.DecisionHash, ticket.ArtifactID, ticket.Token,
			object.SHA256, object.Bytes, s.Bucket, object.VersionID}
	} else if scope.Purpose == "v23_dust_holdout" {
		query = "SELECT photo.complete_v23_reviewed_dust_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.Review,
			scope.Actor, scope.AuthSession, scope.DecisionHash, ticket.ArtifactID, ticket.Token,
			object.SHA256, object.Bytes, s.Bucket, object.VersionID}
	} else if scope.Purpose == "v23_detail_holdout" {
		query = "SELECT photo.complete_v23_detail_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.Project,
			scope.Actor, scope.AuthSession, ticket.ArtifactID, ticket.Token,
			object.SHA256, object.Bytes, s.Bucket, object.VersionID}
	} else if scope.Purpose == "v23_zero_profile" {
		query = "SELECT photo.complete_v23_training_zero($1,$2,$3,$4,$5,$6,$7,$8,$9)"
		args = []any{scope.Tenant, scope.Run, scope.Owner,
			ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes,
			s.Bucket, object.VersionID}
	}
	if scope.Purpose != "v23_zero_profile" && scope.Purpose != "v23_defect_holdout" && scope.Purpose != "v23_dust_holdout" && scope.Purpose != "v23_detail_holdout" {
		args[3] = purposeArg
	}
	if scope.AssemblyLease != uuid.Nil {
		query = "SELECT photo.complete_v27_semantic_assembly_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)"
		args = []any{scope.Tenant, scope.Run, scope.Owner, scope.AssemblyLease, scope.Purpose, ticket.ArtifactID, ticket.Token, object.SHA256, object.Bytes, s.Bucket, object.VersionID}
		if v2InputPurpose(scope.Purpose) {
			query = "SELECT photo.complete_v27_semantic_transport_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)"
		}
	}
	err = s.DB.QueryRow(ctx, query, args...).Scan(&done)
	if err != nil {
		return Ref{}, err
	}
	if !done {
		return Ref{}, ErrStale
	}
	return Ref{ticket.ArtifactID, object.SHA256}, nil
}
