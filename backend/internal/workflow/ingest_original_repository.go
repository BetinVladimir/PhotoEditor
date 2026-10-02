package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
)

type OriginalScope struct {
	Tenant, Run uuid.UUID
	Ordinal     int64
	Source      review.ArtifactRef
}
type OriginalReservation struct {
	ArtifactID, Token uuid.UUID
	Version           string
	Ready             bool
}
type OriginalRepository struct {
	DB     artifactjson.RowQuerier
	Bucket string
}

func (s OriginalScope) valid() bool {
	return s.Tenant != uuid.Nil && s.Run != uuid.Nil && s.Ordinal >= 0 && s.Ordinal <= 9007199254740991 && validExpansionRef(s.Source)
}
func (r OriginalRepository) Reserve(ctx context.Context, s OriginalScope) (OriginalReservation, error) {
	if r.DB == nil || !s.valid() || len(r.Bucket) < 3 || len(r.Bucket) > 63 {
		return OriginalReservation{}, errors.New("invalid original reservation")
	}
	var outcome string
	var id, token *uuid.UUID
	var version *string
	err := r.DB.QueryRow(ctx, "SELECT outcome,artifact_id,token,object_version FROM photo.reserve_ingest_original($1,$2,$3,$4,$5,$6)", s.Tenant, s.Run, s.Ordinal, s.Source.ID, s.Source.SHA256, r.Bucket).Scan(&outcome, &id, &token, &version)
	if err != nil {
		return OriginalReservation{}, err
	}
	if outcome != "created" && outcome != "resumed" && outcome != "replayed" {
		return OriginalReservation{}, errors.New("original reservation unavailable")
	}
	if id == nil || token == nil || *id == uuid.Nil || *token == uuid.Nil || *id == s.Source.ID {
		return OriginalReservation{}, errors.New("invalid original reservation result")
	}
	result := OriginalReservation{ArtifactID: *id, Token: *token, Ready: outcome == "replayed"}
	if result.Ready {
		if version == nil || *version == "" || *version == "null" || len(*version) > 1024 {
			return OriginalReservation{}, errors.New("invalid original version")
		}
		result.Version = *version
	} else if version != nil {
		return OriginalReservation{}, errors.New("unexpected original version")
	}
	return result, nil
}
func (r OriginalRepository) Complete(ctx context.Context, s OriginalScope, res OriginalReservation, version string) (bool, error) {
	if r.DB == nil || !s.valid() || res.ArtifactID == uuid.Nil || res.ArtifactID == s.Source.ID || res.Token == uuid.Nil || version == "" || version == "null" || len(version) > 1024 {
		return false, errors.New("invalid original completion")
	}
	var ok bool
	err := r.DB.QueryRow(ctx, "SELECT photo.complete_ingest_original($1,$2,$3,$4,$5,$6)", s.Tenant, s.Run, s.Ordinal, res.ArtifactID, res.Token, version).Scan(&ok)
	return ok, err
}
