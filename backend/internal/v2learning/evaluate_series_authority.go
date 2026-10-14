package v2learning

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SQLFrozenEvaluateSeriesReader reauthorizes the completed Fit and current
// Apply source around every version-pinned object read.
type SQLFrozenEvaluateSeriesReader struct {
	DB      *pgxpool.Pool // photo_scheduler
	Objects LearningEvaluateObjectReader
	Bucket  string
}

type frozenEvaluateSeriesLocation struct {
	Origin FrozenSeriesHoldoutEvidence
	Object objectstore.Object
	Bucket string
}

func (s SQLFrozenEvaluateSeriesReader) locate(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (frozenEvaluateSeriesLocation, error) {
	if s.DB == nil || s.Bucket == "" || !validLearningEvaluateLease(lease) ||
		eventID == uuid.Nil || caseRef.SeriesEvidence == nil ||
		caseRef.DefectEvidence != nil || caseRef.DustEvidence != nil ||
		caseRef.DetailEvidence != nil ||
		!validManifestRef(*caseRef.SeriesEvidence) {
		return frozenEvaluateSeriesLocation{}, ErrInvalidTrainingDraft
	}
	ref := *caseRef.SeriesEvidence
	stable, err := SeriesHoldoutRef(lease.RunID, eventID, ref.SHA256)
	if err != nil || stable != ref {
		return frozenEvaluateSeriesLocation{}, ErrInvalidTrainingDraft
	}
	location := frozenEvaluateSeriesLocation{Origin: FrozenSeriesHoldoutEvidence{
		SchemaVersion: 1, TenantID: lease.TenantID, EventID: eventID}}
	err = s.DB.QueryRow(ctx, `SELECT asset_version_id,source_id,source_sha,
 apply_run_id,item_id,producer_task_id,stage_id,stage_sha,params_id,
 params_sha,series_id,series_sha,bucket,object_key,object_version,bytes
 FROM photo.v23_evaluate_series_authority($1,$2,$3,$4,$5,$6,$7)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		eventID, ref.ArtifactID, ref.SHA256).Scan(
		&location.Origin.AssetVersionID,
		&location.Origin.SourceImage.ArtifactID,
		&location.Origin.SourceImage.SHA256,
		&location.Origin.ApplyRunID, &location.Origin.ItemID,
		&location.Origin.ProducerTaskID,
		&location.Origin.StageImage.ArtifactID,
		&location.Origin.StageImage.SHA256,
		&location.Origin.Params.ArtifactID,
		&location.Origin.Params.SHA256,
		&location.Origin.Series.ArtifactID,
		&location.Origin.Series.SHA256,
		&location.Bucket, &location.Object.Key,
		&location.Object.VersionID, &location.Object.Bytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return frozenEvaluateSeriesLocation{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return frozenEvaluateSeriesLocation{}, err
	}
	if location.Bucket != s.Bucket || location.Object.Key == "" ||
		location.Object.VersionID == "" ||
		location.Object.VersionID == "null" ||
		location.Object.Bytes < 2 || location.Object.Bytes > 1<<20 ||
		location.Origin.AssetVersionID != caseRef.Binding.AssetVersionID ||
		location.Origin.SourceImage != caseRef.WorkingImage ||
		location.Origin.SourceImage != caseRef.Binding.Source ||
		!validFrozenSeriesHoldout(location.Origin) {
		return frozenEvaluateSeriesLocation{}, ErrInvalidTrainingDraft
	}
	location.Object.SHA256 = ref.SHA256
	location.Object.ContentType = "application/json"
	return location, nil
}

type frozenEvaluateSeriesNested struct {
	ItemID uuid.UUID
	Image  v2input.Ref
	Object objectstore.Object
	Bucket string
}

func (s SQLFrozenEvaluateSeriesReader) nested(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	evidence v2input.Ref, role string,
	ref v2input.Ref) (frozenEvaluateSeriesNested, error) {
	if s.DB == nil || s.Bucket == "" || !validLearningEvaluateLease(lease) ||
		eventID == uuid.Nil || !validManifestRef(evidence) ||
		!validManifestRef(ref) ||
		(role != "stage_image" && role != "params" &&
			role != "series" && role != "stats") {
		return frozenEvaluateSeriesNested{}, ErrInvalidTrainingDraft
	}
	var located frozenEvaluateSeriesNested
	var itemID, imageID *uuid.UUID
	var imageSHA *string
	var media string
	err := s.DB.QueryRow(ctx, `SELECT item_id,image_id,image_sha,bucket,
 object_key,object_version,bytes,media_type
 FROM photo.v23_evaluate_series_nested($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		eventID, evidence.ArtifactID, evidence.SHA256, role,
		ref.ArtifactID, ref.SHA256).Scan(&itemID, &imageID, &imageSHA,
		&located.Bucket, &located.Object.Key,
		&located.Object.VersionID, &located.Object.Bytes, &media)
	if errors.Is(err, pgx.ErrNoRows) {
		return frozenEvaluateSeriesNested{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return frozenEvaluateSeriesNested{}, err
	}
	if located.Bucket != s.Bucket || located.Object.Key == "" ||
		located.Object.VersionID == "" ||
		located.Object.VersionID == "null" ||
		located.Object.Bytes < 2 || located.Object.Bytes > 1<<20 ||
		media != "application/json" {
		return frozenEvaluateSeriesNested{}, ErrInvalidTrainingDraft
	}
	if role == "stats" {
		if itemID == nil || *itemID == uuid.Nil || imageID == nil ||
			imageSHA == nil {
			return frozenEvaluateSeriesNested{}, ErrInvalidTrainingDraft
		}
		located.ItemID = *itemID
		located.Image = v2input.Ref{ArtifactID: *imageID, SHA256: *imageSHA}
		if !validManifestRef(located.Image) {
			return frozenEvaluateSeriesNested{}, ErrInvalidTrainingDraft
		}
	} else if itemID != nil || imageID != nil || imageSHA != nil {
		return frozenEvaluateSeriesNested{}, ErrInvalidTrainingDraft
	}
	located.Object.SHA256 = ref.SHA256
	located.Object.ContentType = media
	return located, nil
}

func (s SQLFrozenEvaluateSeriesReader) loadNested(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	evidence v2input.Ref, role string,
	ref v2input.Ref) ([]byte, frozenEvaluateSeriesNested, error) {
	before, err := s.nested(ctx, lease, eventID, evidence, role, ref)
	if err != nil {
		return nil, frozenEvaluateSeriesNested{}, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before.Object, ref)
	if err != nil {
		return nil, frozenEvaluateSeriesNested{}, err
	}
	after, err := s.nested(ctx, lease, eventID, evidence, role, ref)
	if err != nil {
		return nil, frozenEvaluateSeriesNested{}, err
	}
	if before != after {
		return nil, frozenEvaluateSeriesNested{}, ErrTrainingDraftConflict
	}
	return body, before, nil
}

func (s SQLFrozenEvaluateSeriesReader) Load(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (FrozenEvaluateSeriesInputs, error) {
	if s.Objects == nil || caseRef.SeriesEvidence == nil {
		return FrozenEvaluateSeriesInputs{}, ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	ref := *caseRef.SeriesEvidence
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before.Object, ref)
	if err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	after, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	if before != after {
		return FrozenEvaluateSeriesInputs{}, ErrTrainingDraftConflict
	}
	evidence, err := DecodeFrozenSeriesHoldoutEvidence(body, ref, before.Origin)
	if err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	if err := ValidateFrozenSeriesCase(lease, eventID, caseRef,
		evidence); err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	if _, _, err := s.loadNested(ctx, lease, eventID, ref,
		"stage_image", evidence.StageImage); err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	paramsBody, _, err := s.loadNested(ctx, lease, eventID, ref,
		"params", evidence.Params)
	if err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	seriesBody, _, err := s.loadNested(ctx, lease, eventID, ref,
		"series", evidence.Series)
	if err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	var params frozenSeriesParams
	if !decodePinnedSeriesJSON(paramsBody, evidence.Params.SHA256, &params) ||
		!validSeriesStatsRefs(params.Stats) {
		return FrozenEvaluateSeriesInputs{}, ErrInvalidTrainingDraft
	}
	proof := FrozenEvaluateSeriesInputs{Evidence: evidence,
		ParamsBody: paramsBody, SeriesBody: seriesBody,
		StatsBodies: make([][]byte, len(params.Stats)),
		Entries: []v2input.Entry{
			{Ref: ref, Contract: "FrozenSeriesHoldoutEvidence"},
			{Ref: evidence.StageImage, Contract: "WorkingImage"},
			{Ref: evidence.Params, Contract: "SeriesParams"},
			{Ref: evidence.Series, Contract: "SeriesCorrections"},
		}}
	for i, statRef := range params.Stats {
		statBody, located, err := s.loadNested(ctx, lease, eventID,
			ref, "stats", statRef)
		if err != nil {
			return FrozenEvaluateSeriesInputs{}, err
		}
		var stat frozenSeriesStats
		if len(statBody) > 32768 ||
			!decodePinnedSeriesJSON(statBody, statRef.SHA256, &stat) ||
			stat.ItemID != located.ItemID || stat.Image != located.Image {
			return FrozenEvaluateSeriesInputs{}, ErrInvalidTrainingDraft
		}
		proof.StatsBodies[i] = statBody
		proof.Entries = append(proof.Entries,
			v2input.Entry{Ref: statRef, Contract: "SeriesStats"})
	}
	if err := validateFrozenSeriesEvaluateInputs(lease, eventID,
		caseRef, proof); err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	final, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateSeriesInputs{}, err
	}
	if before != final {
		return FrozenEvaluateSeriesInputs{}, ErrTrainingDraftConflict
	}
	return proof, nil
}
