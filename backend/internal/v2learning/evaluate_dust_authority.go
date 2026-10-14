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

// SQLFrozenEvaluateDustReader checks the live expansion, event, consent and
// unique accepted Apply review on both sides of exact-version object IO.
type SQLFrozenEvaluateDustReader struct {
	DB      *pgxpool.Pool
	Objects LearningEvaluateObjectReader
	Bucket  string
}

type frozenEvaluateDustLocation struct {
	Expected DustHoldoutExpected
	Object   objectstore.Object
	Bucket   string
}

type FrozenEvaluateDustInputs struct {
	Evidence FrozenDustHoldoutEvidence
	Entries  [4]v2input.Entry
}

func validateFrozenDustEvaluateInputs(lease LearningEvaluateLease,
	eventID uuid.UUID, caseRef TrainingRenderCase,
	proof FrozenEvaluateDustInputs) error {
	if err := ValidateFrozenDustHoldoutCase(lease, eventID, caseRef,
		proof.Evidence); err != nil {
		return err
	}
	expected := [4]v2input.Entry{
		{Ref: *caseRef.DustEvidence, Contract: "FrozenDustHoldoutEvidence"},
		{Ref: proof.Evidence.StageImage, Contract: "WorkingImage"},
		{Ref: proof.Evidence.Proposal, Contract: "DustCandidates"},
		{Ref: proof.Evidence.Params, Contract: "Params"},
	}
	for index, entry := range proof.Entries {
		if entry != expected[index] || !validManifestRef(entry.Ref) {
			return ErrInvalidTrainingDraft
		}
		for previous := 0; previous < index; previous++ {
			if proof.Entries[previous].Ref.ArtifactID == entry.Ref.ArtifactID {
				return ErrInvalidTrainingDraft
			}
		}
	}
	return nil
}

func (s SQLFrozenEvaluateDustReader) locate(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (frozenEvaluateDustLocation, error) {
	if s.DB == nil || s.Bucket == "" || !validLearningEvaluateLease(lease) ||
		eventID == uuid.Nil || caseRef.DustEvidence == nil ||
		caseRef.DefectEvidence != nil || !validManifestRef(*caseRef.DustEvidence) {
		return frozenEvaluateDustLocation{}, ErrInvalidTrainingDraft
	}
	ref := *caseRef.DustEvidence
	stable, err := ReviewedDustHoldoutRef(lease.RunID, eventID, ref.SHA256)
	if err != nil || stable != ref {
		return frozenEvaluateDustLocation{}, ErrInvalidTrainingDraft
	}
	location := frozenEvaluateDustLocation{Expected: DustHoldoutExpected{
		TenantID: lease.TenantID, EventID: eventID,
		Protection: caseRef.Protected}}
	err = s.DB.QueryRow(ctx, `SELECT review_id,review_run_id,actor_id,
 decision_hash,asset_version_id,image_id,image_sha,
 stage_image_id,stage_image_sha,proposal_id,proposal_sha,
 params_id,params_sha,bucket,object_key,object_version,bytes
 FROM photo.v23_evaluate_reviewed_dust_authority($1,$2,$3,$4,$5,$6,$7)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		eventID, ref.ArtifactID, ref.SHA256).Scan(
		&location.Expected.ReviewID, &location.Expected.RunID,
		&location.Expected.ActorID, &location.Expected.DecisionHash,
		&location.Expected.AssetVersionID,
		&location.Expected.Image.ArtifactID, &location.Expected.Image.SHA256,
		&location.Expected.StageImage.ArtifactID,
		&location.Expected.StageImage.SHA256,
		&location.Expected.Proposal.ArtifactID,
		&location.Expected.Proposal.SHA256,
		&location.Expected.Params.ArtifactID,
		&location.Expected.Params.SHA256,
		&location.Bucket, &location.Object.Key,
		&location.Object.VersionID, &location.Object.Bytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return frozenEvaluateDustLocation{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return frozenEvaluateDustLocation{}, err
	}
	if location.Bucket != s.Bucket || location.Object.Key == "" ||
		location.Object.VersionID == "" ||
		location.Object.VersionID == "null" ||
		location.Object.Bytes < 2 || location.Object.Bytes > 1<<20 ||
		location.Expected.AssetVersionID != caseRef.Binding.AssetVersionID ||
		location.Expected.Image != caseRef.WorkingImage ||
		location.Expected.Image != caseRef.Binding.Source ||
		!validManifestRef(location.Expected.StageImage) ||
		!validManifestRef(location.Expected.Proposal) ||
		!validManifestRef(location.Expected.Params) ||
		!validSHA(location.Expected.DecisionHash) {
		return frozenEvaluateDustLocation{}, ErrInvalidTrainingDraft
	}
	location.Object.SHA256 = ref.SHA256
	location.Object.ContentType = "application/json"
	return location, nil
}

func (s SQLFrozenEvaluateDustReader) Load(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (FrozenEvaluateDustInputs, error) {
	if s.Objects == nil || caseRef.DustEvidence == nil {
		return FrozenEvaluateDustInputs{}, ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateDustInputs{}, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before.Object,
		*caseRef.DustEvidence)
	if err != nil {
		return FrozenEvaluateDustInputs{}, err
	}
	after, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateDustInputs{}, err
	}
	if before != after {
		return FrozenEvaluateDustInputs{}, ErrTrainingDraftConflict
	}
	evidence, err := DecodeFrozenDustHoldoutEvidence(body,
		*caseRef.DustEvidence, before.Expected)
	if err != nil {
		return FrozenEvaluateDustInputs{}, err
	}
	proof := FrozenEvaluateDustInputs{Evidence: evidence, Entries: [4]v2input.Entry{
		{Ref: *caseRef.DustEvidence, Contract: "FrozenDustHoldoutEvidence"},
		{Ref: evidence.StageImage, Contract: "WorkingImage"},
		{Ref: evidence.Proposal, Contract: "DustCandidates"},
		{Ref: evidence.Params, Contract: "Params"},
	}}
	if err := validateFrozenDustEvaluateInputs(lease, eventID,
		caseRef, proof); err != nil {
		return FrozenEvaluateDustInputs{}, err
	}
	return proof, nil
}
