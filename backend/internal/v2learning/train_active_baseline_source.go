package v2learning

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// TrainingActiveBaselineSource resolves the current active profile only for
// a new draft. A pending draft must retain the ref already in its manifest.
type TrainingActiveBaselineSource struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

type TrainingActiveBaseline struct {
	VersionID   uuid.UUID
	Ref         v2input.Ref
	TrainingSHA string
	Profile     v2draft.CandidateProfile
}

// LoadActive returns nil only when the profile has no active version. A
// broken, disabled or unavailable active version is a conflict, not a zero
// baseline: silently falling back would change the training objective.
func (s TrainingActiveBaselineSource) LoadActive(ctx context.Context,
	start TrainingStart) (*TrainingActiveBaseline, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		start.TenantID == uuid.Nil || start.ProjectID == uuid.Nil ||
		start.ProfileID == uuid.Nil || start.ActorID == uuid.Nil ||
		start.AuthSessionID == uuid.Nil {
		return nil, ErrInvalidTrainingDraft
	}
	var found TrainingActiveBaseline
	var bucket string
	var object objectstore.Object
	var active bool
	var versionID, artifactID *uuid.UUID
	err := s.DB.QueryRow(ctx, `SELECT has_active,version_id,training_sha,
	 artifact_id,artifact_sha,bucket,object_key,object_version,bytes,
	 media_type FROM photo.v23_training_active_baseline($1,$2,$3,$4,$5)`,
		start.TenantID, start.ProjectID, start.ProfileID, start.ActorID,
		start.AuthSessionID).Scan(&active, &versionID, &found.TrainingSHA,
		&artifactID, &found.Ref.SHA256, &bucket, &object.Key,
		&object.VersionID, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTrainingDraftConflict
	}
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, nil
	}
	if versionID == nil || artifactID == nil {
		return nil, ErrTrainingDraftConflict
	}
	found.VersionID, found.Ref.ArtifactID = *versionID, *artifactID
	if found.VersionID == uuid.Nil || !validSHA(found.TrainingSHA) ||
		!validManifestRef(found.Ref) || bucket != s.Bucket {
		return nil, ErrInvalidTrainingDraft
	}
	object.SHA256 = found.Ref.SHA256
	body, err := ReadFrozenRootDocument(ctx, s.Objects, object, found.Ref)
	if err != nil {
		return nil, err
	}
	profile, err := verifyActiveBaselineBytes(body, start.ProfileID,
		found.TrainingSHA)
	if err != nil {
		return nil, err
	}
	found.Profile = profile
	return &found, nil
}

func verifyActiveBaselineBytes(body []byte, profileID uuid.UUID,
	trainingSHA string) (v2draft.CandidateProfile, error) {
	profile, err := v2draft.DecodeCandidateProfile(body)
	if err != nil || profile.ProfileID != profileID ||
		profile.TrainingSHA != trainingSHA ||
		profile.TrainingSHA == strings.Repeat("0", 64) {
		return v2draft.CandidateProfile{}, ErrInvalidTrainingDraft
	}
	return profile, nil
}
