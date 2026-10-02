package v2learning

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// BuildTrainingCaseEvidence derives the target from verified immutable recipe
// bytes. Features and image/mask refs must come from a scoped descriptor and
// artifact resolver; no client-provided target enters the manifest.
func BuildTrainingCaseFromDescriptor(runID uuid.UUID,
	candidate TrainingCandidate, descriptor TrainingDescriptor,
	render TrainingRenderCase, compiled CompiledTrainingRecipes,
	iccLock, modelLock string) (TrainingCaseEvidence, error) {
	if descriptor.AnalysisID == uuid.Nil || descriptor.Ordinal < 0 ||
		descriptor.Ordinal > 4999 || !validManifestRef(descriptor.Shard) ||
		!validManifestRef(descriptor.Faces) ||
		(render.Faces != (v2input.Ref{}) && render.Faces != descriptor.Faces) {
		return TrainingCaseEvidence{}, ErrInvalidTrainingExample
	}
	render.Faces = descriptor.Faces
	render.DescriptorShard = descriptor.Shard
	render.DescriptorAnalysisID = descriptor.AnalysisID
	render.DescriptorOrdinal = descriptor.Ordinal
	return BuildTrainingCaseEvidence(runID, candidate, descriptor.Features,
		render, compiled, iccLock, modelLock)
}

func BuildTrainingCaseEvidence(runID uuid.UUID, candidate TrainingCandidate,
	features [12]float64, render TrainingRenderCase,
	compiled CompiledTrainingRecipes, iccLock, modelLock string) (
	TrainingCaseEvidence, error) {
	var empty TrainingCaseEvidence
	if runID == uuid.Nil || candidate.EventID == uuid.Nil ||
		candidate.SessionID == uuid.Nil || candidate.ApprovalID == nil ||
		!candidate.ConsentLive || !candidate.SourceReady ||
		!candidate.BaseReady || !candidate.ApprovedReady ||
		!validSHA(iccLock) || !validSHA(modelLock) ||
		!validManifestRef(render.WorkingImage) ||
		!validManifestRef(render.Protected) ||
		!validManifestRef(render.Faces) ||
		!validManifestRef(render.DescriptorShard) ||
		render.DescriptorAnalysisID == uuid.Nil ||
		render.DescriptorOrdinal < 0 || render.DescriptorOrdinal > 4999 ||
		render.Binding.AssetVersionID != candidate.AssetVersionID ||
		render.Binding.Source != render.WorkingImage ||
		!validSHA(render.Binding.CanvasHash) {
		return empty, ErrInvalidTrainingExample
	}
	base, err := DecodeFrozenRecipe(compiled.BaseBody)
	if err != nil || base != compiled.Base {
		return empty, ErrInvalidTrainingExample
	}
	approved, err := DecodeFrozenRecipe(compiled.ApprovedBody)
	if err != nil || approved != compiled.Approved ||
		base.Source != render.Binding || approved.Source != render.Binding ||
		base.BaseStyle != approved.BaseStyle ||
		base.ICCLockSHA != iccLock || approved.ICCLockSHA != iccLock ||
		base.ModelLockSHA != modelLock || approved.ModelLockSHA != modelLock {
		return empty, ErrInvalidTrainingExample
	}
	baseSum := sha256.Sum256(compiled.BaseBody)
	approvedSum := sha256.Sum256(compiled.ApprovedBody)
	if compiled.BaseSHA != hex.EncodeToString(baseSum[:]) ||
		compiled.ApprovedSHA != hex.EncodeToString(approvedSum[:]) {
		return empty, ErrInvalidTrainingExample
	}
	baseRef, err := TrainingFrozenRef(runID, candidate.EventID, "base",
		compiled.BaseSHA)
	if err != nil {
		return empty, err
	}
	approvedRef, err := TrainingFrozenRef(runID, candidate.EventID,
		"approved", compiled.ApprovedSHA)
	if err != nil {
		return empty, err
	}
	target := v2draft.CorrectionVector{
		EV:   approved.EffectiveDelta.EV - base.EffectiveDelta.EV,
		LogR: approved.EffectiveDelta.LogR - base.EffectiveDelta.LogR,
		LogG: approved.EffectiveDelta.LogG - base.EffectiveDelta.LogG,
		LogB: approved.EffectiveDelta.LogB - base.EffectiveDelta.LogB,
		SaturationDelta: approved.EffectiveDelta.SaturationDelta -
			base.EffectiveDelta.SaturationDelta,
		RetouchDelta: approved.EffectiveDelta.RetouchDelta -
			base.EffectiveDelta.RetouchDelta,
	}
	source := EligibleTrainingExample{EventID: candidate.EventID,
		SessionID:        candidate.SessionID,
		SourceContentSHA: candidate.SourceContentSHA,
		SourceLineageSHA: candidate.SourceLineageSHA,
		ApprovedAt:       candidate.ApprovedAt, Features: features,
		Target: [6]float64{target.EV, target.LogR, target.LogG,
			target.LogB, target.SaturationDelta, target.RetouchDelta}}
	if !validTrainingExample(source) ||
		(render.EffectiveMask == nil) != (render.ConsentSnapshot == nil) ||
		len(render.RegionMasks) > 128 {
		return empty, ErrInvalidTrainingExample
	}
	render.BaseRecipe, render.ApprovedRecipe = baseRef, approvedRef
	render.BaseDocument = v2input.Ref{ArtifactID: candidate.BaseArtifactID,
		SHA256: candidate.BaseSHA}
	render.ApprovedDocument = v2input.Ref{
		ArtifactID: candidate.ApprovedArtifactID,
		SHA256:     candidate.ApprovedSHA}
	render.ApprovalID = *candidate.ApprovalID
	render.SourceLineageSHA = candidate.SourceLineageSHA
	return TrainingCaseEvidence{Source: source,
		Resolved: ResolvedTrainingCase{Candidate: candidate, Render: render},
		Frozen:   &compiled}, nil
}
