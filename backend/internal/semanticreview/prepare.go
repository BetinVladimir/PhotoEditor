package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PrepareCurrentReview composes scoped source loading, bounded versioned object
// reads, post-I/O reauthorization and typed content validation. It has no writes.
func PrepareCurrentReview(ctx context.Context, load PlanSourceLoader, read PlanObjectReader, budgetBytes int64, modelSHA, datasetSHA string) (PreparedReview, error) {
	if !shaPattern.MatchString(modelSHA) || !shaPattern.MatchString(datasetSHA) {
		return PreparedReview{}, ErrInvalid
	}
	sources, documents, err := ReadCurrentPlanDocuments(ctx, load, read, budgetBytes)
	if err != nil {
		return PreparedReview{}, err
	}
	prepared, err := PrepareReviewDocuments(sources, documents, modelSHA, datasetSHA)
	if err != nil {
		return PreparedReview{}, err
	}
	if err = ctx.Err(); err != nil {
		return PreparedReview{}, err
	}
	return prepared, nil
}

type PreparedReview struct {
	Sources    PlanSources
	PlanRef    v2input.Ref
	IntentRef  v2input.Ref
	Intent     v2input.SemanticEditIntent
	SnapshotID uuid.UUID
	Targets    []InitialTarget
	documents  []PlanDocument
	modelSHA   string
	datasetSHA string
}

// PrepareReviewDocuments validates content after the post-I/O authorization
// fence. Model/dataset pins must come from trusted workflow qualification.
// Persistence still needs live catalog/producer checks under repository locks.
func PrepareReviewDocuments(sources PlanSources, documents []PlanDocument, modelSHA, datasetSHA string) (PreparedReview, error) {
	var empty PreparedReview
	if ValidatePlanSources(sources) != nil || !shaPattern.MatchString(modelSHA) || !shaPattern.MatchString(datasetSHA) || len(documents) != len(sources.Objects) {
		return empty, ErrInvalid
	}
	expected := map[PlanSource]bool{}
	for _, source := range sources.Objects {
		expected[source] = true
	}
	byContract := map[string]PlanDocument{}
	candidates := map[v2input.Ref]PlanDocument{}
	for _, document := range documents {
		if !expected[document.Source] || int64(len(document.Body)) != document.Source.Bytes {
			return empty, ErrInvalid
		}
		delete(expected, document.Source)
		sum := sha256.Sum256(document.Body)
		if hex.EncodeToString(sum[:]) != document.Source.Ref.SHA256 {
			return empty, ErrInvalid
		}
		if document.Source.Contract == "TransferCandidate" {
			candidates[document.Source.Ref] = document
		} else {
			byContract[document.Source.Contract] = document
		}
	}
	if len(expected) != 0 {
		return empty, ErrInvalid
	}
	params, err := v2input.DecodeSemanticAssembleParams(byContract["Params"].Body)
	if err != nil || params.ModelLockSHA != modelSHA || params.Intent != byContract["EditIntent"].Source.Ref || params.Calibration != byContract["Calibration"].Source.Ref || len(params.Candidates) != len(candidates) {
		return empty, ErrInvalid
	}
	intent, err := v2input.DecodeSemanticEditIntent(byContract["EditIntent"].Body)
	if err != nil {
		return empty, ErrInvalid
	}
	intentSHA, err := canonical.Hash(intent)
	if err != nil || intentSHA != params.Intent.SHA256 {
		return empty, ErrInvalid
	}
	snapshotDocument := byContract["InputSnapshot"]
	snapshot, err := v2input.DecodePinnedSessionInputSnapshot(snapshotDocument.Source.Ref, snapshotDocument.Body)
	if err != nil || snapshot.SnapshotID != intent.TargetSnapshotID {
		return empty, ErrInvalid
	}
	calibration := byContract["Calibration"]
	if v2input.VerifySemanticCalibration(calibration.Source.Ref, calibration.Body, modelSHA, datasetSHA) != nil {
		return empty, ErrInvalid
	}
	candidateDocuments := make([]v2input.SemanticCandidateDocument, len(params.Candidates))
	for n, ref := range params.Candidates {
		document, exists := candidates[ref]
		if !exists {
			return empty, ErrInvalid
		}
		candidateDocuments[n] = v2input.SemanticCandidateDocument{Ref: ref, Body: document.Body}
	}
	plan := byContract["SemanticPlan"]
	targets, err := CompileInitialTargets(plan.Source.Ref, plan.Body, params, snapshotDocument.Source.Ref, snapshotDocument.Body, candidateDocuments)
	if err != nil {
		return empty, err
	}
	owned := sources
	owned.Objects = append([]PlanSource(nil), sources.Objects...)
	proof := make([]PlanDocument, len(documents))
	for n, document := range documents {
		proof[n] = PlanDocument{Source: document.Source, Body: append([]byte(nil), document.Body...)}
	}
	return PreparedReview{Sources: owned, PlanRef: plan.Source.Ref, IntentRef: params.Intent, Intent: intent, SnapshotID: snapshot.SnapshotID, Targets: targets, documents: proof, modelSHA: modelSHA, datasetSHA: datasetSHA}, nil
}
