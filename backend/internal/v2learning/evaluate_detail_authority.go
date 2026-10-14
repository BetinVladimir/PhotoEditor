package v2learning

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SQLFrozenEvaluateDetailReader reauthorizes the linked Apply producer around
// exact-version evidence IO under the live Evaluate expansion lease.
type SQLFrozenEvaluateDetailReader struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

type frozenEvaluateDetailLocation struct {
	Origin PortraitDetailHoldoutOrigin
	Object objectstore.Object
	Bucket string
}

type FrozenEvaluateDetailInputs struct {
	Evidence FrozenPortraitDetailHoldoutEvidence
	Entries  []v2input.Entry
}

// verifyNested refuses any input that is no longer a ready, versioned object
// of the linked Apply producer. The function returns no permission to read an
// arbitrary JSON-referenced object.
func (s SQLFrozenEvaluateDetailReader) verifyNested(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	evidenceRef v2input.Ref, entries []v2input.Entry) error {
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		role := "mask"
		switch index {
		case 0:
			role = "image"
		case 1:
			role = "params"
		case 2:
			role = "resolved"
		}
		var bucket, key, version, media string
		var bytes int64
		err := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes,
 media_type FROM photo.v23_evaluate_detail_nested($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
			eventID, evidenceRef.ArtifactID, evidenceRef.SHA256, role,
			entry.Ref.ArtifactID, entry.Ref.SHA256).Scan(
			&bucket, &key, &version, &bytes, &media)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTrainingDraftConflict
		}
		if err != nil {
			return err
		}
		if bucket != s.Bucket || key == "" || version == "" ||
			version == "null" || bytes < 2 ||
			(role != "mask" && (media != "application/json" || bytes > 1<<20)) ||
			(role == "mask" && media != "application/octet-stream" &&
				media != "application/x-npy") {
			return ErrInvalidTrainingDraft
		}
	}
	return nil
}

func (s SQLFrozenEvaluateDetailReader) locate(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (frozenEvaluateDetailLocation, error) {
	if s.DB == nil || s.Bucket == "" || !validLearningEvaluateLease(lease) ||
		eventID == uuid.Nil || caseRef.DetailEvidence == nil ||
		caseRef.DefectEvidence != nil || caseRef.DustEvidence != nil ||
		!validManifestRef(*caseRef.DetailEvidence) {
		return frozenEvaluateDetailLocation{}, ErrInvalidTrainingDraft
	}
	ref := *caseRef.DetailEvidence
	stable, err := PortraitDetailHoldoutRef(lease.RunID, eventID,
		ref.SHA256)
	if err != nil || stable != ref {
		return frozenEvaluateDetailLocation{}, ErrInvalidTrainingDraft
	}
	location := frozenEvaluateDetailLocation{Origin: PortraitDetailHoldoutOrigin{
		TenantID: lease.TenantID, EventID: eventID,
		Protection: caseRef.Protected}}
	err = s.DB.QueryRow(ctx, `SELECT asset_version_id,source_id,source_sha,
 producer_run_id,producer_task_id,producer_result_id,producer_attempt_id,
 image_id,image_sha,params_id,params_sha,resolved_id,resolved_sha,
 bucket,object_key,object_version,bytes
 FROM photo.v23_evaluate_detail_authority($1,$2,$3,$4,$5,$6,$7)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		eventID, ref.ArtifactID, ref.SHA256).Scan(
		&location.Origin.AssetVersionID,
		&location.Origin.Source.ArtifactID,
		&location.Origin.Source.SHA256,
		&location.Origin.RunID, &location.Origin.TaskID,
		&location.Origin.ResultID, &location.Origin.AttemptID,
		&location.Origin.Image.ArtifactID,
		&location.Origin.Image.SHA256,
		&location.Origin.Params.ArtifactID,
		&location.Origin.Params.SHA256,
		&location.Origin.Resolved.ArtifactID,
		&location.Origin.Resolved.SHA256,
		&location.Bucket, &location.Object.Key,
		&location.Object.VersionID, &location.Object.Bytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return frozenEvaluateDetailLocation{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return frozenEvaluateDetailLocation{}, err
	}
	if location.Bucket != s.Bucket || location.Object.Key == "" ||
		location.Object.VersionID == "" || location.Object.VersionID == "null" ||
		location.Object.Bytes < 2 || location.Object.Bytes > 1<<20 ||
		location.Origin.AssetVersionID != caseRef.Binding.AssetVersionID ||
		location.Origin.Source != caseRef.WorkingImage ||
		location.Origin.Source != caseRef.Binding.Source ||
		location.Origin.Image == location.Origin.Source ||
		!validManifestRef(location.Origin.Image) ||
		!validManifestRef(location.Origin.Params) ||
		!validManifestRef(location.Origin.Resolved) {
		return frozenEvaluateDetailLocation{}, ErrInvalidTrainingDraft
	}
	location.Object.SHA256 = ref.SHA256
	location.Object.ContentType = "application/json"
	return location, nil
}

func (s SQLFrozenEvaluateDetailReader) Load(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (FrozenEvaluateDetailInputs, error) {
	if s.Objects == nil || caseRef.DetailEvidence == nil {
		return FrozenEvaluateDetailInputs{}, ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateDetailInputs{}, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before.Object,
		*caseRef.DetailEvidence)
	if err != nil {
		return FrozenEvaluateDetailInputs{}, err
	}
	after, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateDetailInputs{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return FrozenEvaluateDetailInputs{}, ErrTrainingDraftConflict
	}
	var parsed FrozenPortraitDetailHoldoutEvidence
	if json.Unmarshal(body, &parsed) != nil {
		return FrozenEvaluateDetailInputs{}, ErrInvalidTrainingDraft
	}
	before.Origin.MaskArtifacts = parsed.MaskArtifacts
	evidence, err := DecodeFrozenPortraitDetailHoldoutEvidence(body,
		*caseRef.DetailEvidence, before.Origin)
	if err != nil {
		return FrozenEvaluateDetailInputs{}, err
	}
	if err := ValidateFrozenPortraitDetailCase(lease, eventID,
		caseRef, evidence); err != nil {
		return FrozenEvaluateDetailInputs{}, err
	}
	entries := []v2input.Entry{
		{Ref: *caseRef.DetailEvidence,
			Contract: "FrozenPortraitDetailHoldoutEvidence"},
		{Ref: evidence.Image, Contract: "WorkingImage"},
		{Ref: evidence.Params, Contract: "PortraitParams"},
		{Ref: evidence.Resolved, Contract: "ResolvedStyle"},
	}
	for _, mask := range evidence.MaskArtifacts {
		entries = append(entries, v2input.Entry{Ref: mask,
			Contract: "NpyArray"})
	}
	proof := FrozenEvaluateDetailInputs{Evidence: evidence, Entries: entries}
	if err := validateFrozenDetailEvaluateInputs(lease, eventID,
		caseRef, proof); err != nil {
		return FrozenEvaluateDetailInputs{}, err
	}
	if err := s.verifyNested(ctx, lease, eventID,
		*caseRef.DetailEvidence, entries[1:]); err != nil {
		return FrozenEvaluateDetailInputs{}, err
	}
	return proof, nil
}
