package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// RenderExecutionChoice is supplied by server runtime selection. Valid syntax
// alone does not qualify the selected provider or grant activation.
type RenderExecutionChoice struct {
	Backend, RuntimeSHA, AlphaMode string
	TileSize                       int
	Provider                       RenderProviderPeak
}

func (c RenderExecutionChoice) validate() error {
	if (c.Backend != "cuda" && c.Backend != "cpu" && c.Backend != "mps") || len(c.RuntimeSHA) != 64 {
		return v2input.ErrInvalid
	}
	for _, ch := range c.RuntimeSHA {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return v2input.ErrInvalid
		}
	}
	_, err := RenderResources(1, 1, 0, 0, c.TileSize, c.AlphaMode, c.Provider)
	return err
}

type RenderSelectionItem struct {
	Binding v2input.SessionVersionBinding
	Recipe  v2input.Ref
}

func validateRenderSelectionItem(item RenderSelectionItem) error {
	if item.Binding.AssetVersionID == uuid.Nil || !validLayerArtifactRef(item.Recipe) || !validLayerArtifactRef(item.Binding.Source) || item.Recipe.ArtifactID == item.Binding.Source.ArtifactID {
		return v2input.ErrInvalid
	}
	canvas, err := hex.DecodeString(item.Binding.CanvasHash)
	if err != nil || len(canvas) != 32 || hex.EncodeToString(canvas) != item.Binding.CanvasHash {
		return v2input.ErrInvalid
	}
	return nil
}

// RenderRequestLoader reads only scoped, pinned immutable documents. Catalog
// must be bound to the selected project and asset version; descendants retain
// their actual recipe occurrence instead of being promoted to independent roots.
type RenderRequestLoader struct {
	Catalog v2draft.ArtifactCatalog
	Objects v2input.SessionAnalysisObjectReader
}

func (s RenderRequestLoader) Load(ctx context.Context, tenant uuid.UUID, who auth.Identity, item RenderSelectionItem, execution RenderExecutionChoice) (RenderDraftRequest, error) {
	invalid := func() (RenderDraftRequest, error) { return RenderDraftRequest{}, v2input.ErrInvalid }
	if err := ctx.Err(); err != nil {
		return RenderDraftRequest{}, err
	}
	if s.Catalog == nil || s.Objects == nil || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || item.Binding.AssetVersionID == uuid.Nil || !validLayerArtifactRef(item.Recipe) || !validLayerArtifactRef(item.Binding.Source) {
		return invalid()
	}
	if err := validateRenderSelectionItem(item); err != nil {
		return RenderDraftRequest{}, err
	}
	if err := execution.validate(); err != nil {
		return RenderDraftRequest{}, err
	}
	read := func(node v2draft.LayerRef) ([]byte, error) {
		obj, err := s.Catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return nil, err
		}
		if obj.ContentType != "application/json" || obj.Bytes < 2 || obj.Bytes > 1<<20 || obj.SHA256 != node.Ref.SHA256 || obj.VersionID == "" || obj.VersionID == "null" {
			return nil, v2input.ErrInvalid
		}
		reader, err := s.Objects.Open(ctx, obj)
		if err != nil {
			return nil, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, obj.Bytes+1))
		closeErr := reader.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		sum := sha256.Sum256(raw)
		wire, err := canonical.Bytes(raw)
		if int64(len(raw)) != obj.Bytes || hex.EncodeToString(sum[:]) != node.Ref.SHA256 || err != nil || !bytes.Equal(wire, raw) {
			return nil, v2input.ErrInvalid
		}
		// Repeat live scope after IO, requiring the same complete immutable metadata.
		current, err := s.Catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return nil, err
		}
		if current != obj {
			return nil, v2input.ErrExpired
		}
		return raw, nil
	}
	recipe, err := read(v2draft.LayerRef{Ref: item.Recipe, Contract: "FrozenRenderRecipe"})
	if err != nil {
		return RenderDraftRequest{}, err
	}
	source, err := read(v2draft.LayerRef{Ref: item.Binding.Source, Contract: "WorkingImage"})
	if err != nil {
		return RenderDraftRequest{}, err
	}
	evidence, err := CompileRenderEvidence(item.Binding, item.Recipe, recipe, source)
	if err != nil {
		return RenderDraftRequest{}, err
	}
	request := RenderDraftRequest{Tenant: tenant, Actor: who, Binding: item.Binding, RecipeRef: item.Recipe, RecipeRaw: recipe, SourceRaw: source, Backend: execution.Backend, RuntimeSHA: execution.RuntimeSHA, AlphaMode: execution.AlphaMode, TileSize: execution.TileSize, Provider: execution.Provider}
	children, err := v2draft.DocumentChildren(recipe, "FrozenRenderRecipe", item.Recipe)
	if err != nil {
		return RenderDraftRequest{}, err
	}
	for _, child := range children {
		if child.Contract != "LayerStack" && child.Contract != "MaskRef" {
			continue
		}
		raw, err := read(child)
		if err != nil {
			return RenderDraftRequest{}, err
		}
		if child.Contract == "LayerStack" {
			request.LayerRaw = raw
		} else {
			request.ProtectionRaw = raw
		}
	}
	if err := ValidateRenderProtectionEvidence(evidence, request.ProtectionRaw); err != nil {
		return RenderDraftRequest{}, err
	}
	if _, err := CompileRenderExecutionProfile(item.Binding, item.Recipe, recipe, source, request.LayerRaw, execution.Backend, execution.RuntimeSHA, execution.TileSize, execution.AlphaMode, execution.Provider); err != nil {
		return RenderDraftRequest{}, err
	}
	if err := ctx.Err(); err != nil {
		return RenderDraftRequest{}, err
	}
	return request, nil
}
