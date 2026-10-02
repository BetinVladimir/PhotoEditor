package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

const trainingZeroProfilePurpose = "v23_zero_profile"
const trainingZeroProfileName = "v23-zero-profile/"

type TrainingZeroBaseline struct {
	Ref     v2input.Ref
	Body    []byte
	Profile v2draft.CandidateProfile
}

// BuildTrainingZeroBaseline gives an untrained profile a versioned baseline
// whose prediction is exactly zero for every valid scene12_v1 descriptor.
// The artifact ID is known before the draft exists; publication must use the
// same UUIDv5 derivation and reject any SHA not pinned in draft recovery.
func BuildTrainingZeroBaseline(runID, profileID uuid.UUID) (
	TrainingZeroBaseline, error) {
	if runID == uuid.Nil || profileID == uuid.Nil {
		return TrainingZeroBaseline{}, ErrInvalidTrainingDraft
	}
	profile := v2draft.CandidateProfile{SchemaVersion: 1,
		ProfileID: profileID, TrainingSHA: strings.Repeat("0", 64),
		FeatureOrder: "scene12_v1", RidgeLambda: .1}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	body, err := canonical.Marshal(profile)
	if err != nil || len(body) < 2 || len(body) > 1048576 {
		return TrainingZeroBaseline{}, ErrInvalidTrainingDraft
	}
	if _, err := v2draft.DecodeCandidateProfile(body); err != nil {
		return TrainingZeroBaseline{}, ErrInvalidTrainingDraft
	}
	sum := sha256.Sum256(body)
	id := uuid.NewSHA1(runID,
		[]byte(trainingZeroProfileName+profileID.String()))
	return TrainingZeroBaseline{Ref: v2input.Ref{ArtifactID: id,
		SHA256: hex.EncodeToString(sum[:])}, Body: body,
		Profile: profile}, nil
}

// IsTrainingZeroBaseline matches both the run-scoped ID and exact canonical
// body. A hash-only match cannot substitute a foreign profile's baseline.
func IsTrainingZeroBaseline(runID, profileID uuid.UUID,
	ref v2input.Ref) bool {
	zero, err := BuildTrainingZeroBaseline(runID, profileID)
	return err == nil && zero.Ref == ref
}
