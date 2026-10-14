package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type PreparedReviewedDefectHoldout struct {
	Document         FrozenDefectHoldoutEvidence
	Body             []byte
	SHA256           string
	ProtectionPixels v2input.Ref
}

// PrepareReviewedDefectHoldout joins the SQL-scoped review source to exact
// approved training recipe, style, image, Params and protected MaskRef bytes.
// The result is publication material only; no artifact ref or authority is
// conferred until the event/source and protection are rechecked in SQL and the
// immutable document is registered in the learning workflow closure.
func PrepareReviewedDefectHoldout(candidate TrainingCandidate,
	runID uuid.UUID, render TrainingRenderCase, recipes CompiledTrainingRecipes,
	source ReviewedDefectSource, styleRaw, resolvedRaw, paramsRaw,
	imageRaw, protectedRaw []byte) (PreparedReviewedDefectHoldout, error) {
	invalid := func() (PreparedReviewedDefectHoldout, error) {
		return PreparedReviewedDefectHoldout{}, ErrInvalidTrainingDraft
	}
	if candidate.EventID == [16]byte{} || candidate.AssetVersionID == [16]byte{} ||
		!candidate.ConsentLive || !candidate.SourceReady ||
		!candidate.BaseReady || !candidate.ApprovedReady ||
		candidate.ConsentID == [16]byte{} || candidate.ConsentEpoch < 1 ||
		!validManifestRef(render.WorkingImage) ||
		!validManifestRef(render.Protected) || render.DefectEvidence != nil ||
		render.WorkingImage != source.Image ||
		render.Binding.Source != source.Image ||
		render.Binding.AssetVersionID != candidate.AssetVersionID ||
		candidate.SourceContentSHA != source.Image.SHA256 {
		return invalid()
	}
	if !validReviewedDefectRecipeMaterial(runID, candidate.EventID, render, recipes) {
		return invalid()
	}
	base, err := DecodeFrozenRecipe(recipes.BaseBody)
	if err != nil || base != recipes.Base {
		return invalid()
	}
	approved, err := DecodeFrozenRecipe(recipes.ApprovedBody)
	if err != nil || approved != recipes.Approved ||
		base.Source != render.Binding || approved.Source != render.Binding ||
		base.BaseStyle != approved.BaseStyle {
		return invalid()
	}
	if err := ValidateReviewedDefectStyle(styleRaw, resolvedRaw,
		base.BaseStyle, source.Resolved, source.Image); err != nil {
		return invalid()
	}
	mask, pixels, err := DecodeReviewedDefectRepairParams(paramsRaw,
		imageRaw, source)
	if err != nil || !validManifestRef(pixels) || len(protectedRaw) < 2 ||
		len(protectedRaw) > 1<<20 {
		return invalid()
	}
	digest := sha256.Sum256(protectedRaw)
	if hex.EncodeToString(digest[:]) != render.Protected.SHA256 {
		return invalid()
	}
	canonicalMask, err := canonical.Marshal(mask)
	if err != nil || !bytes.Equal(canonicalMask, protectedRaw) {
		return invalid()
	}
	parsedMask, err := review.ParseMaskRef(protectedRaw)
	if err != nil || parsedMask.Artifact != mask.Artifact ||
		parsedMask.Canvas != mask.Canvas {
		return invalid()
	}
	document, body, sha, err := CompileFrozenDefectHoldoutEvidence(
		source.Decision, candidate.EventID, candidate.AssetVersionID,
		source.Image, render.Protected, source.Params)
	if err != nil {
		return invalid()
	}
	return PreparedReviewedDefectHoldout{Document: document,
		Body: body, SHA256: sha, ProtectionPixels: pixels}, nil
}

func validReviewedDefectRecipeMaterial(runID, eventID uuid.UUID, render TrainingRenderCase,
	recipes CompiledTrainingRecipes) bool {
	if runID == uuid.Nil || eventID == uuid.Nil ||
		!validManifestRef(render.BaseRecipe) ||
		!validManifestRef(render.ApprovedRecipe) ||
		render.BaseRecipe.ArtifactID == render.ApprovedRecipe.ArtifactID ||
		!validSHA(recipes.BaseSHA) || !validSHA(recipes.ApprovedSHA) ||
		render.BaseRecipe.SHA256 != recipes.BaseSHA ||
		render.ApprovedRecipe.SHA256 != recipes.ApprovedSHA ||
		len(recipes.BaseBody) < 2 || len(recipes.ApprovedBody) < 2 ||
		len(recipes.BaseBody) > 1<<20 || len(recipes.ApprovedBody) > 1<<20 {
		return false
	}
	expectedBase, expectedApproved, err := recipes.Refs(runID, eventID)
	if err != nil || render.BaseRecipe != expectedBase ||
		render.ApprovedRecipe != expectedApproved {
		return false
	}
	baseDigest := sha256.Sum256(recipes.BaseBody)
	approvedDigest := sha256.Sum256(recipes.ApprovedBody)
	return hex.EncodeToString(baseDigest[:]) == recipes.BaseSHA &&
		hex.EncodeToString(approvedDigest[:]) == recipes.ApprovedSHA
}
