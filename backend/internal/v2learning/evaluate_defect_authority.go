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

// SQLFrozenEvaluateDefectReader rechecks lease, consent, event/source and
// current selected review on both sides of exact-version object IO.
type SQLFrozenEvaluateDefectReader struct {
	DB      *pgxpool.Pool
	Objects LearningEvaluateObjectReader
	Bucket  string
}

type frozenEvaluateDefectLocation struct {
	Expected DefectHoldoutExpected
	Object   objectstore.Object
	Bucket   string
	Inputs   [5]v2input.Entry
}

type FrozenEvaluateDefectInputs struct {
	Evidence FrozenDefectHoldoutEvidence
	Entries  [6]v2input.Entry
}

func validateFrozenDefectEvaluateInputs(lease LearningEvaluateLease,
	eventID uuid.UUID, caseRef TrainingRenderCase,
	proof FrozenEvaluateDefectInputs) error {
	if err := ValidateFrozenDefectHoldoutCase(lease, eventID, caseRef,
		proof.Evidence); err != nil {
		return err
	}
	if caseRef.DefectEvidence == nil {
		return ErrInvalidTrainingDraft
	}
	expectedContracts := [6]string{"FrozenDefectHoldoutEvidence", "Params",
		"DefectCandidates", "DefectLabelMap", "DefectReview", "ResolvedStyle"}
	for index, entry := range proof.Entries {
		if entry.Contract != expectedContracts[index] ||
			!validManifestRef(entry.Ref) {
			return ErrInvalidTrainingDraft
		}
		for previous := 0; previous < index; previous++ {
			if proof.Entries[previous].Ref.ArtifactID == entry.Ref.ArtifactID {
				return ErrInvalidTrainingDraft
			}
		}
	}
	if proof.Entries[0].Ref != *caseRef.DefectEvidence ||
		proof.Entries[1].Ref != proof.Evidence.Params ||
		proof.Entries[2].Ref.ArtifactID != proof.Evidence.Selection.Proposal.ID ||
		proof.Entries[2].Ref.SHA256 != proof.Evidence.Selection.Proposal.SHA256 ||
		proof.Entries[3].Ref.ArtifactID != proof.Evidence.Selection.LabelMap.ID ||
		proof.Entries[3].Ref.SHA256 != proof.Evidence.Selection.LabelMap.SHA256 {
		return ErrInvalidTrainingDraft
	}
	return nil
}

func (s SQLFrozenEvaluateDefectReader) locate(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (frozenEvaluateDefectLocation, error) {
	if s.DB == nil || !validLearningEvaluateLease(lease) || eventID == uuid.Nil ||
		caseRef.DefectEvidence == nil || !validManifestRef(*caseRef.DefectEvidence) ||
		s.Bucket == "" {
		return frozenEvaluateDefectLocation{}, ErrInvalidTrainingDraft
	}
	ref := *caseRef.DefectEvidence
	expectedID, err := ReviewedDefectHoldoutRef(lease.RunID, eventID, ref.SHA256)
	if err != nil || expectedID != ref {
		return frozenEvaluateDefectLocation{}, ErrInvalidTrainingDraft
	}
	location := frozenEvaluateDefectLocation{Expected: DefectHoldoutExpected{
		TenantID: lease.TenantID, EventID: eventID,
		Protection: caseRef.Protected}}
	location.Inputs[0].Contract = "Params"
	location.Inputs[1].Contract = "DefectCandidates"
	location.Inputs[2].Contract = "DefectLabelMap"
	location.Inputs[3].Contract = "DefectReview"
	location.Inputs[4].Contract = "ResolvedStyle"
	err = s.DB.QueryRow(ctx, `SELECT review_id,review_run_id,actor_id,
 decision_hash,asset_version_id,image_id,image_sha,params_id,params_sha,
 proposal_id,proposal_sha,labels_id,labels_sha,selection_id,selection_sha,
 resolved_id,resolved_sha,
 bucket,object_key,object_version,bytes
 FROM photo.v23_evaluate_reviewed_defect_authority($1,$2,$3,$4,$5,$6,$7)`,
		lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
		eventID, ref.ArtifactID, ref.SHA256).Scan(
		&location.Expected.ReviewID, &location.Expected.RunID,
		&location.Expected.ActorID, &location.Expected.DecisionHash,
		&location.Expected.AssetVersionID,
		&location.Expected.Image.ArtifactID, &location.Expected.Image.SHA256,
		&location.Expected.Params.ArtifactID, &location.Expected.Params.SHA256,
		&location.Inputs[1].Ref.ArtifactID, &location.Inputs[1].Ref.SHA256,
		&location.Inputs[2].Ref.ArtifactID, &location.Inputs[2].Ref.SHA256,
		&location.Inputs[3].Ref.ArtifactID, &location.Inputs[3].Ref.SHA256,
		&location.Inputs[4].Ref.ArtifactID, &location.Inputs[4].Ref.SHA256,
		&location.Bucket, &location.Object.Key, &location.Object.VersionID,
		&location.Object.Bytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return frozenEvaluateDefectLocation{}, ErrTrainingDraftConflict
	}
	if err != nil {
		return frozenEvaluateDefectLocation{}, err
	}
	if location.Bucket != s.Bucket || location.Object.Key == "" ||
		location.Object.VersionID == "" || location.Object.VersionID == "null" ||
		location.Object.Bytes < 2 || location.Object.Bytes > 1<<20 ||
		location.Expected.AssetVersionID != caseRef.Binding.AssetVersionID ||
		location.Expected.Image != caseRef.WorkingImage ||
		!validManifestRef(location.Expected.Protection) ||
		!validManifestRef(location.Expected.Params) ||
		!validSHA(location.Expected.DecisionHash) {
		return frozenEvaluateDefectLocation{}, ErrInvalidTrainingDraft
	}
	location.Inputs[0].Ref = location.Expected.Params
	for _, entry := range location.Inputs {
		if !validManifestRef(entry.Ref) {
			return frozenEvaluateDefectLocation{}, ErrInvalidTrainingDraft
		}
	}
	location.Object.SHA256 = ref.SHA256
	location.Object.ContentType = "application/json"
	return location, nil
}

func (s SQLFrozenEvaluateDefectReader) Load(ctx context.Context,
	lease LearningEvaluateLease, eventID uuid.UUID,
	caseRef TrainingRenderCase) (FrozenEvaluateDefectInputs, error) {
	if s.Objects == nil || caseRef.DefectEvidence == nil {
		return FrozenEvaluateDefectInputs{}, ErrInvalidTrainingDraft
	}
	before, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateDefectInputs{}, err
	}
	body, err := ReadFrozenRootDocument(ctx, s.Objects, before.Object,
		*caseRef.DefectEvidence)
	if err != nil {
		return FrozenEvaluateDefectInputs{}, err
	}
	after, err := s.locate(ctx, lease, eventID, caseRef)
	if err != nil {
		return FrozenEvaluateDefectInputs{}, err
	}
	if before != after {
		return FrozenEvaluateDefectInputs{}, ErrTrainingDraftConflict
	}
	evidence, err := DecodeFrozenDefectHoldoutEvidence(body,
		*caseRef.DefectEvidence, before.Expected)
	if err != nil {
		return FrozenEvaluateDefectInputs{}, err
	}
	if err := ValidateFrozenDefectHoldoutCase(lease, eventID, caseRef, evidence); err != nil {
		return FrozenEvaluateDefectInputs{}, err
	}
	if evidence.Selection.Proposal.ID != before.Inputs[1].Ref.ArtifactID ||
		evidence.Selection.Proposal.SHA256 != before.Inputs[1].Ref.SHA256 ||
		evidence.Selection.LabelMap.ID != before.Inputs[2].Ref.ArtifactID ||
		evidence.Selection.LabelMap.SHA256 != before.Inputs[2].Ref.SHA256 {
		return FrozenEvaluateDefectInputs{}, ErrInvalidTrainingDraft
	}
	proof := FrozenEvaluateDefectInputs{Evidence: evidence, Entries: [6]v2input.Entry{
		{Ref: *caseRef.DefectEvidence, Contract: "FrozenDefectHoldoutEvidence"},
		before.Inputs[0], before.Inputs[1], before.Inputs[2],
		before.Inputs[3], before.Inputs[4],
	}}
	if err := validateFrozenDefectEvaluateInputs(lease, eventID,
		caseRef, proof); err != nil {
		return FrozenEvaluateDefectInputs{}, err
	}
	return proof, nil
}
