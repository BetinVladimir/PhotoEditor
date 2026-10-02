package v2learning

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type TrainingStageResult struct {
	Outcome     string
	Snapshot    TrainingArtifactSnapshot
	ManifestRef artifactjson.Ref
	SnapshotRef artifactjson.Ref
}

type FitStageResult struct {
	TaskID  uuid.UUID
	Params  artifactjson.Ref
	Closure FitClosure
}

// StageFitTask runs only after Stage has committed the immutable snapshot.
// Publication is deliberately outside the transaction; a retry republishes
// the same content-addressed document before the idempotent SQL stage.
func (s TrainingInputStager) StageFitTask(ctx context.Context,
	draft TrainingDraftPrepared, policy LearningPolicy) (FitStageResult, error) {
	if s.DB == nil || s.Publisher.Repository == nil || s.Publisher.Objects == nil {
		return FitStageResult{}, ErrInvalidTrainingDraft
	}
	task, err := v2input.TaskID(draft.WorkflowID, "learning.fit", 0)
	if err != nil {
		return FitStageResult{}, err
	}
	params, err := PublishFitParams(ctx, s.Publisher, draft, policy)
	if err != nil {
		return FitStageResult{}, err
	}
	closure, err := CompileFitClosure(draft, policy, params)
	if err != nil {
		return FitStageResult{}, err
	}
	err = database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var staged bool
		if err := tx.QueryRow(ctx, `SELECT photo.stage_v23_learning_fit(
		 $1,$2,$3,$4,$5,$6,$7::jsonb)`, draft.TenantID, draft.RunID,
			task, params.ID, params.SHA256, closure.InputDigest,
			string(closure.Profile)).Scan(&staged); err != nil {
			return err
		}
		if !staged {
			return ErrTrainingDraftConflict
		}
		return nil
	})
	if err != nil {
		return FitStageResult{}, err
	}
	return FitStageResult{TaskID: task, Params: params, Closure: closure}, nil
}

// TrainingInputStager is the crash-safe first half of TrainProfile admission.
// Graph tasks are intentionally compiled only after this returns a sealed
// snapshot; a separate graph commit produces the API's final JobTicket.
type TrainingInputStager struct {
	DB            *pgxpool.Pool // photo_scheduler connection
	Publisher     artifactjson.Publisher
	FrozenRecipes map[uuid.UUID]CompiledTrainingRecipes
}

func (s TrainingInputStager) Stage(ctx context.Context,
	draft TrainingDraftPrepared) (TrainingStageResult, error) {
	var result TrainingStageResult
	if s.DB == nil || s.Publisher.Repository == nil ||
		s.Publisher.Objects == nil || draft.RunID == uuid.Nil ||
		draft.ProfileID == uuid.Nil || len(draft.ManifestBody) < 2 ||
		len(draft.SnapshotBody) < 2 ||
		len(s.FrozenRecipes) != len(draft.Manifest.Examples) {
		return result, ErrInvalidTrainingDraft
	}
	seen := make(map[uuid.UUID]bool, len(draft.Manifest.Examples))
	for _, example := range draft.Manifest.Examples {
		pair, okay := s.FrozenRecipes[example.EventID]
		if !okay || seen[example.EventID] {
			return result, ErrInvalidTrainingDraft
		}
		seen[example.EventID] = true
		if _, _, err := ValidateTrainingFrozenPair(draft, example.EventID,
			pair); err != nil {
			return result, err
		}
	}
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var err error
		result.Outcome, err = BeginTrainingDraftInTransaction(ctx, tx, draft)
		return err
	})
	if err != nil {
		return TrainingStageResult{}, err
	}
	if result.Outcome == "replayed" {
		return result, nil
	}
	if IsTrainingZeroBaseline(draft.RunID, draft.ProfileID,
		draft.Manifest.Baseline) {
		zero, err := BuildTrainingZeroBaseline(draft.RunID, draft.ProfileID)
		if err != nil {
			return TrainingStageResult{}, err
		}
		published, err := s.Publisher.Publish(ctx, artifactjson.Scope{
			Tenant: draft.TenantID, Run: draft.RunID, Owner: draft.ProfileID,
			Purpose: trainingZeroProfilePurpose}, zero.Body)
		if err != nil {
			return TrainingStageResult{}, err
		}
		if published.ID != zero.Ref.ArtifactID ||
			published.SHA256 != zero.Ref.SHA256 {
			return TrainingStageResult{}, ErrTrainingDraftConflict
		}
	}
	for _, example := range draft.Manifest.Examples {
		pair, okay := s.FrozenRecipes[example.EventID]
		if !okay {
			return TrainingStageResult{}, ErrInvalidTrainingDraft
		}
		if _, _, err := PublishTrainingFrozenPair(ctx, s.Publisher,
			draft, example.EventID, pair); err != nil {
			return TrainingStageResult{}, err
		}
	}
	manifestScope := artifactjson.Scope{Tenant: draft.TenantID,
		Run: draft.RunID, Owner: draft.ProfileID,
		Purpose: trainingManifestPurpose}
	result.ManifestRef, err = s.Publisher.Publish(ctx, manifestScope,
		draft.ManifestBody)
	if err != nil {
		return TrainingStageResult{}, err
	}
	manifestID, _ := TrainingDocumentID(draft.RunID, trainingManifestPurpose)
	if result.ManifestRef.ID != manifestID ||
		result.ManifestRef.SHA256 != draft.ManifestSHA {
		return TrainingStageResult{}, ErrTrainingDraftConflict
	}
	snapshotScope := artifactjson.Scope{Tenant: draft.TenantID,
		Run: draft.RunID, Owner: draft.RunID,
		Purpose: trainingSnapshotPurpose}
	result.SnapshotRef, err = s.Publisher.Publish(ctx, snapshotScope,
		draft.SnapshotBody)
	if err != nil {
		return TrainingStageResult{}, err
	}
	rootID, _ := TrainingDocumentID(draft.RunID, trainingSnapshotPurpose)
	if result.SnapshotRef.ID != rootID ||
		result.SnapshotRef.SHA256 != draft.SnapshotBodySHA {
		return TrainingStageResult{}, ErrTrainingDraftConflict
	}
	err = database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var err error
		result.Snapshot, err = FreezePublishedTrainingSnapshot(ctx, tx,
			TrainingFreezeRequest{TenantID: draft.TenantID,
				RunID: draft.RunID, ProjectID: draft.ProjectID,
				ActorID: draft.ActorID, AuthSessionID: draft.AuthSessionID,
				ExpectedRevision: draft.ExpectedRevision,
				Manifest:         draft.Manifest, ManifestSHA: draft.ManifestSHA,
				SnapshotBodySHA: draft.SnapshotBodySHA})
		return err
	})
	if err != nil {
		return TrainingStageResult{}, err
	}
	if result.Snapshot.SnapshotID != draft.SnapshotID ||
		result.Snapshot.SnapshotDigest != draft.SnapshotDigest {
		return TrainingStageResult{}, ErrTrainingDraftConflict
	}
	return result, nil
}
