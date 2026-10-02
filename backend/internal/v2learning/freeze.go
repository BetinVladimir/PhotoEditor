package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
)

var ErrTrainingFreezeConflict = errors.New("TRAINING_FREEZE_CONFLICT")

type TrainingFreezeRequest struct {
	TenantID, RunID, ProjectID, ActorID, AuthSessionID uuid.UUID
	ExpectedRevision                                   int64
	Manifest                                           TrainingManifest
	ManifestSHA, SnapshotBodySHA                       string
}

// FreezePublishedTrainingSnapshot runs only on a photo_scheduler transaction.
// The caller must commit that transaction after this returns; no object IO is
// performed under these locks. The SQL function independently checks that both
// backend documents are ready and pins consent links before sealing the row.
func FreezePublishedTrainingSnapshot(ctx context.Context, tx pgx.Tx,
	request TrainingFreezeRequest) (TrainingArtifactSnapshot, error) {
	if tx == nil || request.TenantID == uuid.Nil || request.RunID == uuid.Nil ||
		request.ProjectID == uuid.Nil || request.ActorID == uuid.Nil ||
		request.AuthSessionID == uuid.Nil || request.ExpectedRevision < 1 ||
		!validSHA(request.ManifestSHA) || !validSHA(request.SnapshotBodySHA) {
		return TrainingArtifactSnapshot{}, ErrInvalidTrainingSnapshot
	}
	manifestBody, err := canonical.Marshal(request.Manifest)
	if err != nil || len(manifestBody) < 2 || len(manifestBody) > 8388608 {
		return TrainingArtifactSnapshot{}, ErrInvalidTrainingManifest
	}
	manifestDigest := sha256.Sum256(manifestBody)
	if hex.EncodeToString(manifestDigest[:]) != request.ManifestSHA {
		return TrainingArtifactSnapshot{}, ErrInvalidTrainingManifest
	}
	snapshot, _, rootSHA, err := CompileTrainingArtifactSnapshot(
		request.RunID, request.ProjectID, request.Manifest.ProfileID,
		request.ManifestSHA)
	if err != nil || rootSHA != request.SnapshotBodySHA {
		return TrainingArtifactSnapshot{}, ErrInvalidTrainingSnapshot
	}
	if err := RevalidateTrainingManifest(ctx, tx, request.TenantID,
		request.ActorID, request.AuthSessionID, request.ExpectedRevision,
		request.Manifest); err != nil {
		return TrainingArtifactSnapshot{}, err
	}
	var frozenComplete bool
	if err := tx.QueryRow(ctx, `SELECT photo.v23_training_frozen_complete(
	 $1,$2,$3::jsonb)`, request.TenantID, request.RunID,
		string(manifestBody)).Scan(&frozenComplete); err != nil {
		return TrainingArtifactSnapshot{}, err
	}
	if !frozenComplete {
		return TrainingArtifactSnapshot{}, ErrTrainingFreezeConflict
	}
	var frozen bool
	err = tx.QueryRow(ctx, `SELECT photo.freeze_v23_training_snapshot(
	 $1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)`, request.TenantID,
		request.RunID, request.Manifest.ProfileID, request.ActorID,
		request.AuthSessionID, request.ExpectedRevision,
		string(manifestBody), request.ManifestSHA,
		request.SnapshotBodySHA).Scan(&frozen)
	if err != nil {
		return TrainingArtifactSnapshot{}, err
	}
	if !frozen {
		return TrainingArtifactSnapshot{}, ErrTrainingFreezeConflict
	}
	return snapshot, nil
}
