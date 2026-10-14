package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ReviewedDustSource is populated by a scoped, current SQL review and event
// read. Callers must not build it from request or frozen-document fields.
type ReviewedDustSource struct {
	Decision       DustHoldoutDecision
	AssetVersionID uuid.UUID
	Image          v2input.Ref
	StageImage     v2input.Ref
	Proposal       v2input.Ref
	Params         v2input.Ref
	DetectTaskID   uuid.UUID
	RepairTaskID   uuid.UUID
}

type PreparedReviewedDustHoldout struct {
	Document FrozenDustHoldoutEvidence
	Body     []byte
	SHA256   string
}

// PrepareReviewedDustHoldout binds a current reviewed source to the training
// case and exact registered material. Publication requires separate SQL
// reserve/complete authority; this value is not itself an executable input.
func PrepareReviewedDustHoldout(ctx context.Context, candidate TrainingCandidate,
	trainingRunID uuid.UUID, render TrainingRenderCase,
	recipes CompiledTrainingRecipes, source ReviewedDustSource,
	imageRaw, stageImageRaw, paramsRaw, proposalRaw, protectedRaw []byte) (PreparedReviewedDustHoldout, error) {
	invalid := func() (PreparedReviewedDustHoldout, error) {
		return PreparedReviewedDustHoldout{}, ErrInvalidTrainingDraft
	}
	if err := ctx.Err(); err != nil {
		return PreparedReviewedDustHoldout{}, err
	}
	if candidate.EventID == uuid.Nil || candidate.AssetVersionID == uuid.Nil ||
		!candidate.ConsentLive || !candidate.SourceReady ||
		!candidate.BaseReady || !candidate.ApprovedReady ||
		candidate.ConsentID == uuid.Nil || candidate.ConsentEpoch < 1 ||
		source.AssetVersionID != candidate.AssetVersionID ||
		source.Image != render.WorkingImage ||
		source.Image != render.Binding.Source ||
		render.Binding.AssetVersionID != candidate.AssetVersionID ||
		candidate.SourceContentSHA != source.Image.SHA256 ||
		render.DefectEvidence != nil ||
		!validManifestRef(render.Protected) ||
		!validManifestRef(source.StageImage) ||
		source.StageImage.ArtifactID == source.Image.ArtifactID ||
		!validManifestRef(source.Proposal) ||
		!validManifestRef(source.Params) ||
		!validReviewedDefectRecipeMaterial(trainingRunID,
			candidate.EventID, render, recipes) {
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
	if len(imageRaw) < 2 || len(imageRaw) > 1<<20 ||
		len(stageImageRaw) < 2 || len(stageImageRaw) > 1<<20 ||
		len(protectedRaw) < 2 || len(protectedRaw) > 1<<20 {
		return invalid()
	}
	imageDigest := sha256.Sum256(imageRaw)
	if hex.EncodeToString(imageDigest[:]) != source.Image.SHA256 {
		return invalid()
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return invalid()
	}
	stageDigest := sha256.Sum256(stageImageRaw)
	if hex.EncodeToString(stageDigest[:]) != source.StageImage.SHA256 {
		return invalid()
	}
	stageImage, err := review.ParseWorkingImage(stageImageRaw)
	if err != nil || stageImage.Canvas != image.Canvas {
		return invalid()
	}
	digest := sha256.Sum256(protectedRaw)
	if hex.EncodeToString(digest[:]) != render.Protected.SHA256 {
		return invalid()
	}
	canonicalMask, err := canonical.Bytes(protectedRaw)
	if err != nil || !bytes.Equal(canonicalMask, protectedRaw) {
		return invalid()
	}
	mask, err := review.ParseMaskRef(protectedRaw)
	if err != nil || !mask.Protected || mask.Canvas != image.Canvas {
		return invalid()
	}
	imageChain, err := canonical.Bytes(image.TransformChain)
	if err != nil {
		return invalid()
	}
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil || !bytes.Equal(imageChain, maskChain) {
		return invalid()
	}
	document, body, sha, err := CompileFrozenDustHoldoutEvidence(
		source.Decision, candidate.EventID, candidate.AssetVersionID,
		source.Image, source.StageImage, render.Protected, source.Proposal, source.Params)
	if err != nil || ValidateFrozenDustHoldoutMaterial(ctx,
		document, paramsRaw, proposalRaw) != nil {
		return invalid()
	}
	return PreparedReviewedDustHoldout{Document: document,
		Body: body, SHA256: sha}, nil
}
