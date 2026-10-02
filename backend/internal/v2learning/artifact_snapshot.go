package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

var ErrInvalidTrainingSnapshot = errors.New("INVALID_TRAINING_SNAPSHOT")

const (
	trainingManifestPurpose = "v23_training_manifest"
	trainingSnapshotPurpose = "v23_training_snapshot"
	trainingFitParamsPurpose = "v23_fit_params"
)

type TrainingArtifactItem struct {
	Ordinal  int         `json:"ordinal"`
	Artifact v2input.Ref `json:"artifact"`
	Contract string      `json:"contract"`
	OwnerID  uuid.UUID   `json:"owner_id"`
}

type TrainingArtifactSnapshot struct {
	SchemaVersion  int                    `json:"schema_version"`
	SnapshotID     uuid.UUID              `json:"snapshot_id"`
	SnapshotDigest string                 `json:"snapshot_digest"`
	ProjectID      uuid.UUID              `json:"project_id"`
	Items          []TrainingArtifactItem `json:"items"`
}

// TrainingDocumentID is shared with SQL reservation: UUIDv5(run, purpose).
// Changing this name changes the immutable object key and snapshot digest.
func TrainingDocumentID(run uuid.UUID, purpose string) (uuid.UUID, error) {
	if run == uuid.Nil || (purpose != trainingManifestPurpose &&
		purpose != trainingSnapshotPurpose && purpose != trainingFitParamsPurpose) {
		return uuid.Nil, ErrInvalidTrainingSnapshot
	}
	return uuid.NewSHA1(run, []byte(purpose)), nil
}

// CompileTrainingArtifactSnapshot creates the one-item ArtifactSnapshot after
// manifest bytes are finalized and before creating the draft. The digest is
// JCS SHA-256 of the object with snapshot_digest omitted, as specified by the
// worker contract; the returned body includes that digest.
func CompileTrainingArtifactSnapshot(run, projectID, profileID uuid.UUID,
	manifestSHA string) (TrainingArtifactSnapshot,
	[]byte, string, error) {
	var empty TrainingArtifactSnapshot
	if run == uuid.Nil || projectID == uuid.Nil || profileID == uuid.Nil ||
		!validSHA(manifestSHA) {
		return empty, nil, "", ErrInvalidTrainingSnapshot
	}
	manifestID, err := TrainingDocumentID(run, trainingManifestPurpose)
	if err != nil {
		return empty, nil, "", err
	}
	snapshotID := uuid.NewSHA1(run, []byte("training.snapshot"))
	item := TrainingArtifactItem{Ordinal: 0,
		Artifact: v2input.Ref{ArtifactID: manifestID, SHA256: manifestSHA},
		Contract: "TrainingManifest", OwnerID: profileID}
	withoutDigest := struct {
		SchemaVersion int                    `json:"schema_version"`
		SnapshotID    uuid.UUID              `json:"snapshot_id"`
		ProjectID     uuid.UUID              `json:"project_id"`
		Items         []TrainingArtifactItem `json:"items"`
	}{1, snapshotID, projectID, []TrainingArtifactItem{item}}
	canonicalWithout, err := canonical.Marshal(withoutDigest)
	if err != nil {
		return empty, nil, "", ErrInvalidTrainingSnapshot
	}
	digest := sha256.Sum256(canonicalWithout)
	out := TrainingArtifactSnapshot{SchemaVersion: 1,
		SnapshotID: snapshotID, SnapshotDigest: hex.EncodeToString(digest[:]),
		ProjectID: projectID, Items: []TrainingArtifactItem{item}}
	body, err := canonical.Marshal(out)
	if err != nil || len(body) > 1048576 {
		return empty, nil, "", ErrInvalidTrainingSnapshot
	}
	bodyDigest := sha256.Sum256(body)
	return out, body, hex.EncodeToString(bodyDigest[:]), nil
}
