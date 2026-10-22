package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ReadAuthoredGeometryInputEvidence expands the actually published strict
// Params through the current original/prefix/native occurrence authorities.
// It never creates an inventory by concatenating caller-nominated refs.
func ReadAuthoredGeometryInputEvidence(ctx context.Context, scheduler *pgxpool.Pool, objects SQLAuthoredGeometryPhaseObjects, who auth.Identity, prefix AuthoredGeometryPrefix, compiled CompiledAuthoredGeometryTaskParams, ref v2input.Ref, witnesses AuthoredGeometryScopeWitnesses) (AuthoredGeometryInputEvidence, error) {
	empty := AuthoredGeometryInputEvidence{}
	s := prefix.Context
	if e := ctx.Err(); e != nil {
		return empty, e
	}
	if ValidateAuthoredGeometryTaskParams(compiled, ref) != nil || who.UserID != s.Actor || who.SessionID != s.Session {
		return empty, v2input.ErrInvalid
	}
	objects.current = &authoredGeometryCurrentObjectScope{Scheduler: scheduler, Source: s, Params: compiled, Ref: ref, Witnesses: witnesses}
	catalog, e := NewSQLAuthoredGeometryInputCatalog(ctx, scheduler, objects, who, s.Tenant, s.Parent, s.Instruction.Ordinal)
	if e != nil {
		return empty, e
	}
	raw, e := objects.ReadJSON(ctx, who, s.Tenant, s.Project, s.Parent, s.Instruction.Ordinal, ref)
	if e != nil {
		return empty, e
	}
	if !bytes.Equal(raw, compiled.JSON) {
		return empty, v2input.ErrInvalid
	}
	roots, e := AuthoredGeometryTaskInputRoots(compiled, ref)
	if e != nil {
		return empty, e
	}
	var base struct {
		Original v2input.SessionVersionBinding `json:"original_source"`
	}
	if json.Unmarshal([]byte(prefix.BaseReceiptJCS), &base) != nil || !authoredBindingValid(base.Original) {
		return empty, v2input.ErrInvalid
	}
	resolver := v2draft.ClosureResolver{Catalog: catalog, Objects: objects.Objects}
	inputs, e := resolver.ResolveBoundProvenanceEvidence(ctx, who, s.Tenant, []v2draft.LayerRef{
		{Path: "AuthoredGeometry.original_source", Contract: "WorkingImage", Ref: base.Original.Source, CanvasHash: base.Original.CanvasHash},
		{Path: "AuthoredGeometry.params", Contract: "Params", Ref: ref},
	}, base.Original)
	if e != nil {
		return empty, e
	}
	for _, root := range roots {
		found := false
		for _, entry := range inputs.Entries {
			if entry == root {
				found = true
				break
			}
		}
		if !found {
			return empty, v2input.ErrInvalid
		}
	}
	after, found, e := ReadAuthoredGeometryPrefix(ctx, scheduler, s.Tenant, s.Parent, s.Instruction.Ordinal)
	if e != nil {
		return empty, e
	}
	a, e := canonical.Marshal(prefix)
	if e != nil {
		return empty, e
	}
	b, e := canonical.Marshal(after)
	if e != nil || !found || !bytes.Equal(a, b) {
		return empty, v2input.ErrExpired
	}
	if e = ctx.Err(); e != nil {
		return empty, e
	}
	return AuthoredGeometryInputEvidence{Params: compiled, Inputs: inputs}, nil
}
