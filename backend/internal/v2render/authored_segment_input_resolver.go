package v2render

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// AuthoredSegmentInputEvidence owns the complete scoped typed traversal. It is
// not a substitute for segment Params publication or protected task registration.
type AuthoredSegmentInputEvidence struct {
	Params CompiledAuthoredSegmentParams
	Inputs v2draft.TaskInputEvidence
}

// ReadAuthoredSegmentInputEvidence consumes an ACTUALLY PUBLISHED Params ref.
// PhaseObjects must authorize that exact document under the segment instruction;
// a future UUID or caller-provided body cannot satisfy this IO boundary.
func ReadAuthoredSegmentInputEvidence(ctx context.Context, scheduler *pgxpool.Pool, objects SQLAuthoredPhaseObjects, who auth.Identity, tenant, parent uuid.UUID, ordinal int, paramsRef v2input.Ref) (AuthoredSegmentInputEvidence, error) {
	empty := AuthoredSegmentInputEvidence{}
	catalog, err := NewSQLAuthoredSegmentCatalog(ctx, scheduler, objects, who, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	documents, found, err := ReadAuthoredSegmentDocuments(ctx, scheduler, objects, who, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	if !found {
		return empty, v2input.ErrInvalid
	}
	raw, err := objects.ReadJSON(ctx, who, tenant, catalog.Project, parent, ordinal, paramsRef)
	if err != nil {
		return empty, err
	}
	var params AuthoredSegmentParams
	if decodeAuthoredParams(raw, &params) != nil {
		return empty, v2input.ErrInvalid
	}
	compiled, err := CompileAuthoredSegmentParams(params)
	if err != nil || !bytes.Equal(compiled.JSON, raw) {
		return empty, v2input.ErrInvalid
	}
	expected, err := MaterializeAuthoredSegmentParams(documents.Inventory, params.CurrentLayers, params.ProtectedMask)
	if err != nil || !bytes.Equal(expected.JSON, compiled.JSON) {
		return empty, v2input.ErrInvalid
	}
	roots, err := AuthoredSegmentInputRoots(compiled, paramsRef)
	if err != nil {
		return empty, err
	}
	// The original full recipe/source binding remains the traversal anchor. Each
	// authored WorkingImage occurrence supplies its own scoped target geometry.
	var base struct {
		Original v2input.SessionVersionBinding `json:"original_source"`
	}
	// This is a projection from the already verified immutable base receipt;
	// its complete closed schema is independently parsed by typed traversal.
	if json.Unmarshal([]byte(documents.Inventory.BaseReceiptJCS), &base) != nil || !authoredBindingValid(base.Original) {
		return empty, v2input.ErrInvalid
	}
	catalog.paramsRef = paramsRef
	resolver := v2draft.ClosureResolver{Catalog: catalog, Objects: objects.Objects}
	// Dedicated authored entry: ordinary full-render two-root policy remains
	// unchanged. The actual published strict Params supplies every typed edge;
	// the exact original anchor is walked first to initialize source provenance.
	nodes := []v2draft.LayerRef{
		{Path: "AuthoredSegment.original_source", Contract: "WorkingImage", Ref: base.Original.Source, CanvasHash: base.Original.CanvasHash},
		{Path: "AuthoredSegment.params", Contract: "Params", Ref: paramsRef},
	}
	inputs, err := resolver.ResolveBoundProvenanceEvidence(ctx, who, tenant, nodes, base.Original)
	if err != nil {
		return empty, err
	}
	// Every compiler-owned direct root must occur with its exact typed contract.
	for _, expectedRoot := range roots {
		foundRoot := false
		for _, actual := range inputs.Entries {
			if actual == expectedRoot {
				foundRoot = true
				break
			}
		}
		if !foundRoot {
			return empty, v2input.ErrInvalid
		}
	}
	after, present, err := ReadAuthoredSegmentInventory(ctx, scheduler, tenant, parent, ordinal)
	if err != nil {
		return empty, err
	}
	if !present {
		return empty, v2input.ErrInvalid
	}
	a, err := canonical.Marshal(documents.Inventory)
	if err != nil {
		return empty, err
	}
	b, err := canonical.Marshal(after)
	if err != nil || !bytes.Equal(a, b) {
		return empty, v2input.ErrInvalid
	}
	return AuthoredSegmentInputEvidence{Params: compiled, Inputs: inputs}, nil
}
