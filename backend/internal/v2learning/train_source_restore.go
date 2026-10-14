package v2learning

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type TrainingFrozenReader interface {
	Load(context.Context, TrainingStart, TrainingDraftPrepared,
		uuid.UUID, string, v2input.Ref) ([]byte, error)
}

type SQLTrainingFrozenSource struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

func (s SQLTrainingFrozenSource) Load(ctx context.Context,
	start TrainingStart, draft TrainingDraftPrepared,
	eventID uuid.UUID, side string, ref v2input.Ref) ([]byte, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		draft.RunID == uuid.Nil || draft.ProfileID != start.ProfileID ||
		draft.TenantID != start.TenantID || eventID == uuid.Nil ||
		(side != "base" && side != "approved") || !validManifestRef(ref) {
		return nil, ErrInvalidTrainingDraft
	}
	expected, err := TrainingFrozenRef(draft.RunID, eventID, side,
		ref.SHA256)
	if err != nil || expected != ref {
		return nil, ErrInvalidTrainingDraft
	}
	var object objectstore.Object
	var bucket string
	err = s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes,
	 media_type FROM photo.v23_training_frozen_restore($1,$2,$3,$4,$5,
	 $6,$7,$8,$9)`, start.TenantID, draft.RunID, draft.ProfileID,
		eventID, side, ref.ArtifactID, ref.SHA256, start.ActorID,
		start.AuthSessionID).Scan(&bucket, &object.Key, &object.VersionID,
		&object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTrainingDraftConflict
	}
	if err != nil {
		return nil, err
	}
	if bucket != s.Bucket {
		return nil, ErrInvalidTrainingDraft
	}
	object.SHA256 = ref.SHA256
	return ReadFrozenRootDocument(ctx, s.Objects, object, ref)
}

// Restore reads only immutable FrozenRecipe refs already in the pending
// recovery manifest, then revalidates every byte against that exact draft.
func (s TrainingSourceResolver) Restore(ctx context.Context,
	start TrainingStart, draft TrainingDraftPrepared) (
	map[uuid.UUID]CompiledTrainingRecipes, error) {
 if err:=ctx.Err();err!=nil{return nil,err}
	if start.TenantID != draft.TenantID ||
		start.ProfileID != draft.ProfileID ||
		start.ProjectID != draft.ProjectID ||
		start.ActorID != draft.ActorID ||
		start.AuthSessionID != draft.AuthSessionID ||
		draft.RunID == uuid.Nil || len(draft.Manifest.Examples) < 40 ||
		len(draft.Manifest.Examples) > 5000 {
		return nil, ErrInvalidTrainingDraft
	}
	var recovery trainingDraftRecovery
	if json.Unmarshal(draft.RecoveryBody, &recovery) != nil {
		return nil, ErrInvalidTrainingDraft
	}
	if recovery.FrozenJCS != "" {
  result,err:=frozenRecipesFromRecovery(draft, []byte(recovery.FrozenJCS))
  if err!=nil{return nil,err}
  if err:=ctx.Err();err!=nil{return nil,err}
  return result,nil
	}
	if s.Frozen == nil {
		return nil, ErrInvalidTrainingDraft
	}
	result := make(map[uuid.UUID]CompiledTrainingRecipes,
		len(draft.Manifest.Examples))
	for _, example := range draft.Manifest.Examples {
  if err:=ctx.Err();err!=nil{return nil,err}
		if example.EventID == uuid.Nil ||
			result[example.EventID].BaseBody != nil {
			return nil, ErrInvalidTrainingDraft
		}
		baseRef, approvedRef := example.RenderCase.BaseRecipe,
			example.RenderCase.ApprovedRecipe
		baseBody, err := s.Frozen.Load(ctx, start, draft,
			example.EventID, "base", baseRef)
		if err != nil {
			return nil, err
		}
  if err:=ctx.Err();err!=nil{return nil,err}
  baseBody=append([]byte(nil),baseBody...)
		approvedBody, err := s.Frozen.Load(ctx, start, draft,
			example.EventID, "approved", approvedRef)
		if err != nil {
			return nil, err
		}
  if err:=ctx.Err();err!=nil{return nil,err}
  approvedBody=append([]byte(nil),approvedBody...)
		base, err := DecodeFrozenRecipe(baseBody)
		if err != nil {
			return nil, err
		}
		approved, err := DecodeFrozenRecipe(approvedBody)
		if err != nil {
			return nil, err
		}
		pair := CompiledTrainingRecipes{Base: base, Approved: approved,
			BaseBody: baseBody, ApprovedBody: approvedBody,
			BaseSHA: baseRef.SHA256, ApprovedSHA: approvedRef.SHA256}
		if _, _, err := ValidateTrainingFrozenPair(draft,
			example.EventID, pair); err != nil {
			return nil, err
		}
		result[example.EventID] = pair
	}
 if err:=ctx.Err();err!=nil{return nil,err}
	return result, nil
}

var _ TrainingSource = TrainingSourceResolver{}
