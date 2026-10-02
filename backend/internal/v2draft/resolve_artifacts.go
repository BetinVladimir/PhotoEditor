package v2draft

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// ReadResolvedScalars verifies the recipe closure and reads the exact
// StyleV2 and CorrectionPlan versions used by ResolveRecipe. The closure
// walk proves provenance of nested references before scalar extraction.
func (s ClosureResolver) ReadResolvedScalars(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, raw []byte, binding v2input.SessionVersionBinding,
) (StyleBaseline, v2input.Ref, *ResolvedCorrection, error) {
	if _, err := s.Resolve(ctx, who, tenantID, raw, binding); err != nil {
		return StyleBaseline{}, v2input.Ref{}, nil, err
	}
	var recipe struct {
		BaseStyle          v2input.Ref  `json:"base_style"`
		SessionCorrections *v2input.Ref `json:"session_corrections"`
	}
	if json.Unmarshal(raw, &recipe) != nil {
		return StyleBaseline{}, v2input.Ref{}, nil, v2input.ErrInvalid
	}
	styleBody, err := s.readScopedContract(ctx, who, tenantID,
		LayerRef{Path: "EditingRecipeV3.base_style", Contract: "StyleV2",
			Ref: recipe.BaseStyle})
	if err != nil {
		return StyleBaseline{}, v2input.Ref{}, nil, err
	}
	style, err := DecodeStyleBaseline(styleBody)
	if err != nil {
		return StyleBaseline{}, v2input.Ref{}, nil, err
	}
	if recipe.SessionCorrections == nil {
		return style, recipe.BaseStyle, nil, nil
	}
	body, err := s.readScopedContract(ctx, who, tenantID,
		LayerRef{Path: "EditingRecipeV3.session_corrections", Contract: "CorrectionPlan",
			Ref: *recipe.SessionCorrections})
	if err != nil {
		return StyleBaseline{}, v2input.Ref{}, nil, err
	}
	vector, err := CurrentCorrection(body, binding)
	if err != nil {
		return StyleBaseline{}, v2input.Ref{}, nil, err
	}
	return style, recipe.BaseStyle,
		&ResolvedCorrection{Ref: *recipe.SessionCorrections, Vector: vector}, nil
}

func (s ClosureResolver) readScopedContract(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, node LayerRef) ([]byte, error) {
	if s.Catalog == nil || s.Objects == nil || tenantID == uuid.Nil ||
		!isDocumentRef(node.Ref) {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	object, err := s.Catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return nil, err
	}
	return readJSONArtifact(ctx, s.Objects, object, node.Ref)
}
