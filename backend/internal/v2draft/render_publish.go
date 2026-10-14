package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// PublishFrozenRenderRecipe publishes a complete validated wire through the
// existing immutable upload protocol, then registers SQL source provenance.
// Compilation, style/scalar authority and full-root catalog admission remain
// independent requirements; publication is not task authorization.
func (p Publisher) PublishFrozenRenderRecipe(ctx context.Context, who auth.Identity, tenantID, assetVersionID, documentID uuid.UUID, raw []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if len(raw) < 2 || len(raw) > 1048576 {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	body := append([]byte(nil), raw...)
	recipe, err := DecodeFrozenRenderRecipe(body)
	if err != nil || assetVersionID == uuid.Nil || recipe.Scalars.Source.AssetVersionID != assetVersionID {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	ref, err := p.Publish(ctx, who, tenantID, assetVersionID, documentID, body)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := p.RegisterFrozenRenderRecipe(ctx, who, tenantID, assetVersionID, documentID, ref, body); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}

// CompileAndPublishFrozenRenderRecipe derives the uploaded bytes from the full
// authored wire and trusted resolved inputs. It never accepts caller-authored
// frozen scalar values as a substitute for compiler output. SQL provenance and
// actual source/style/learning authorization remain independent required gates.
func (p Publisher) CompileAndPublishFrozenRenderRecipe(ctx context.Context, who auth.Identity, tenantID, documentID uuid.UUID, authored []byte, inputs ResolvedRecipeInputs, layerBody []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	body, err := BuildFrozenRenderRecipe(authored, inputs, layerBody)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.PublishFrozenRenderRecipe(ctx, who, tenantID, inputs.Binding.AssetVersionID, documentID, body)
}

// CompileAndPublishFrozenRenderRecipeWithBackground resolves the selected
// accepted-style portable image on the server. Caller supplies explicit input
// ICC assumption; the public recipe never supplies an arbitrary artifact pin.
// Whole-module override selection must be reconciled before choosing this path.
func (p Publisher) CompileAndPublishFrozenRenderRecipeWithBackground(ctx context.Context,
	who auth.Identity, tenantID, projectID, documentID uuid.UUID, authored []byte,
	inputs ResolvedRecipeInputs, layerBody []byte, portableID string, assumeProfile *string) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	authored = append([]byte(nil), authored...)
	layerBody = append([]byte(nil), layerBody...)
	inputs.Overrides = append([]ResolvedCorrection(nil), inputs.Overrides...)
	if inputs.Session != nil {
		value := *inputs.Session
		inputs.Session = &value
	}
	if inputs.Learning != nil {
		value := *inputs.Learning
		inputs.Learning = &value
	}
	if assumeProfile != nil {
		value := *assumeProfile
		assumeProfile = &value
	}
	// Reject invalid author/source/scalar/layer evidence before catalog IO.
	if _, err := BuildFrozenRenderRecipe(authored, inputs, layerBody); err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if projectID == uuid.Nil || portableID == "" {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	background, err := (SQLBackgroundAssetResolver{DB: p.DB, Bucket: p.Bucket}).Freeze(ctx, who,
		tenantID, projectID, inputs.BaseStyle, portableID, inputs.ICCLockSHA, assumeProfile)
	if err != nil {
		return v2input.Ref{}, err
	}
	body, err := BuildFrozenRenderRecipeWithBackground(authored, inputs, layerBody, background)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return p.PublishFrozenRenderRecipe(ctx, who, tenantID, inputs.Binding.AssetVersionID, documentID, body)
}

// CompileAndPublishSelectedFrozenRenderRecipe selects imported background from
// hash-verified accepted style and ordered overrides, never from a caller asset
// selector. The caller still provides independently authorized resolved inputs.
func (p Publisher) CompileAndPublishSelectedFrozenRenderRecipe(ctx context.Context,
	who auth.Identity, tenantID, projectID, documentID uuid.UUID, authored []byte,
	inputs ResolvedRecipeInputs, layerBody, styleBody []byte, overrideBodies [][]byte,
	sourceSHA string, assumeProfile *string) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	portableID, err := SelectRenderBackgroundAsset(authored, inputs, styleBody, overrideBodies, sourceSHA)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	merged, _, err := resolveRenderModuleDocuments(authored, inputs, styleBody, overrideBodies, sourceSHA)
	if err != nil {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	inputs.Style = merged
	if portableID == "" {
		if assumeProfile != nil {
			return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
		}
		return p.CompileAndPublishFrozenRenderRecipe(ctx, who, tenantID, documentID, authored, inputs, layerBody)
	}
	return p.CompileAndPublishFrozenRenderRecipeWithBackground(ctx, who, tenantID, projectID, documentID, authored, inputs, layerBody, portableID, assumeProfile)
}

// PublishFrozenRenderDraftRecipe uploads before entering the SQL revision fence.
// It never performs unfenced registration first. The current draft document and
// revision must still match when immutable source registration is committed.
func (p Publisher) PublishFrozenRenderDraftRecipe(ctx context.Context, who auth.Identity,
	tenantID, assetVersionID, documentID uuid.UUID, raw []byte, draftDocumentID uuid.UUID,
	expectedRevision int64) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if len(raw) < 2 || len(raw) > 1048576 || draftDocumentID == uuid.Nil || draftDocumentID == documentID || expectedRevision < 1 {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	body := append([]byte(nil), raw...)
	recipe, err := DecodeFrozenRenderRecipe(body)
	if err != nil || assetVersionID == uuid.Nil || recipe.Scalars.Source.AssetVersionID != assetVersionID {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	ref, err := p.Publish(ctx, who, tenantID, assetVersionID, documentID, body)
	if err != nil {
		return v2input.Ref{}, err
	}
	if err := p.RegisterFrozenRenderDraftRecipe(ctx, who, tenantID, assetVersionID, documentID, ref, body, draftDocumentID, expectedRevision); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}

// CompileAndPublishSelectedFrozenRenderDraftRecipe is the full document-backed
// freeze path. Object reads/uploads finish before the atomic SQL draft fence.
func (p Publisher) CompileAndPublishSelectedFrozenRenderDraftRecipe(ctx context.Context,
	who auth.Identity, tenantID, projectID, documentID, draftDocumentID uuid.UUID, expectedRevision int64,
	authored []byte, inputs ResolvedRecipeInputs, layerBody, styleBody []byte, overrideBodies [][]byte,
	sourceSHA string, assumeProfile *string) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if documentID == uuid.Nil || draftDocumentID == uuid.Nil || documentID == draftDocumentID || expectedRevision < 1 {
		return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	body, err := p.CompileSelectedFrozenRenderRecipe(ctx, who, tenantID, projectID, authored, inputs, layerBody, styleBody, overrideBodies, sourceSHA, assumeProfile)
	if err != nil {
		return v2input.Ref{}, err
	}
	return p.PublishFrozenRenderDraftRecipe(ctx, who, tenantID, inputs.Binding.AssetVersionID, documentID, body, draftDocumentID, expectedRevision)
}

// CompileSelectedFrozenRenderRecipe compiles complete detached document evidence
// and any authorized imported background without uploading or registering it.
// This allows registration and idempotent response to commit in one SQL transaction.
func (p Publisher) CompileSelectedFrozenRenderRecipe(ctx context.Context, who auth.Identity,
	tenantID, projectID uuid.UUID, authored []byte, inputs ResolvedRecipeInputs,
	layerBody, styleBody []byte, overrideBodies [][]byte, sourceSHA string, assumeProfile *string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Retain detached compiler evidence across the authorized catalog lookup.
	authored = append([]byte(nil), authored...)
	layerBody = append([]byte(nil), layerBody...)
	inputs.Overrides = append([]ResolvedCorrection(nil), inputs.Overrides...)
	if inputs.Session != nil {
		value := *inputs.Session
		inputs.Session = &value
	}
	if inputs.Learning != nil {
		value := *inputs.Learning
		inputs.Learning = &value
	}
	if assumeProfile != nil {
		value := *assumeProfile
		assumeProfile = &value
	}
	portableID, err := SelectRenderBackgroundAsset(authored, inputs, styleBody, overrideBodies, sourceSHA)
	if err != nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	merged, _, err := resolveRenderModuleDocuments(authored, inputs, styleBody, overrideBodies, sourceSHA)
	if err != nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	inputs.Style = merged
	// Reject bad layer/learning evidence before resolving any imported object.
	body, err := BuildFrozenRenderRecipe(authored, inputs, layerBody)
	if err != nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if portableID == "" {
		if assumeProfile != nil {
			return nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	} else {
		if projectID == uuid.Nil {
			return nil, domain.NewError("VALIDATION_FAILED", 422)
		}
		background, err := (SQLBackgroundAssetResolver{DB: p.DB, Bucket: p.Bucket}).Freeze(ctx, who, tenantID, projectID, inputs.BaseStyle, portableID, inputs.ICCLockSHA, assumeProfile)
		if err != nil {
			return nil, err
		}
		body, err = BuildFrozenRenderRecipeWithBackground(authored, inputs, layerBody, background)
		if err != nil {
			return nil, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return body, nil
}
