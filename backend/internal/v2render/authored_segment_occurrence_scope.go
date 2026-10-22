package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// segmentOccurrenceParams accepts only the exact current published Params or
// a completed segment producer's Params retained in the adopted prefix.
func (s SQLAuthoredSegmentCatalog) segmentOccurrenceParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, inventory AuthoredSegmentInventory, ref v2input.Ref) (AuthoredSegmentParams, bool, error) {
	empty := AuthoredSegmentParams{}
	current := ref == s.paramsRef && validLayerArtifactRef(s.paramsRef)
	var prior *AuthoredPhaseReceipt
	for _, item := range inventory.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if phase.Kind == "segment" && phase.Params == ref {
			copy := phase
			prior = &copy
			break
		}
	}
	if !current && prior == nil {
		return empty, false, nil
	}
	raw, err := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, ref)
	if err != nil {
		return empty, false, err
	}
	var p AuthoredSegmentParams
	if decodeAuthoredParams(raw, &p) != nil {
		return empty, false, v2input.ErrInvalid
	}
	compiled, err := CompileAuthoredSegmentParams(p)
	if err != nil || !bytes.Equal(compiled.JSON, raw) {
		return empty, false, v2input.ErrInvalid
	}
	if p.TenantID != tenant || p.ParentRunID != s.Parent || p.WorkflowID != inventory.Context.Workflow || p.ProgramSHA256 != inventory.Context.ProgramSHA || p.Recipe != inventory.Context.OriginalRecipe || p.BaseReceipt != inventory.BaseReceipt {
		return empty, false, v2input.ErrInvalid
	}
	if current {
		expected, e := MaterializeAuthoredSegmentParams(inventory, p.CurrentLayers, p.ProtectedMask)
		if e != nil || !bytes.Equal(expected.JSON, raw) {
			return empty, false, v2input.ErrInvalid
		}
	} else if p.Ordinal != prior.Ordinal || p.InstructionID != prior.Instruction || p.Source != prior.InputSource || p.PredecessorSHA256 != prior.PredecessorSHA {
		return empty, false, v2input.ErrInvalid
	}
	if len(p.PhaseReceipts) > len(inventory.Phases) {
		return empty, false, v2input.ErrInvalid
	}
	for i, receipt := range p.PhaseReceipts {
		if receipt != inventory.Phases[i].Receipt {
			return empty, false, v2input.ErrInvalid
		}
	}
	return p, true, nil
}
func (s SQLAuthoredSegmentCatalog) occurrenceInventory(ctx context.Context, who auth.Identity, tenant uuid.UUID) (AuthoredSegmentInventory, error) {
	v, found, err := ReadAuthoredSegmentInventory(ctx, s.Scheduler, tenant, s.Parent, s.Ordinal)
	if err != nil {
		return v, err
	}
	if !found || v.Context.Project != s.Project || v.Context.Actor != who.UserID || v.Context.Session != who.SessionID {
		return v, v2input.ErrInvalid
	}
	return v, nil
}
func (s SQLAuthoredSegmentCatalog) segmentParamRefs(v AuthoredSegmentInventory) []v2input.Ref {
	refs := []v2input.Ref{s.paramsRef}
	for _, item := range v.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) == nil && phase.Kind == "segment" {
			refs = append(refs, phase.Params)
		}
	}
	return refs
}
func (s SQLAuthoredSegmentCatalog) LayerStackBinding(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2input.SessionVersionBinding, bool, error) {
	empty := v2input.SessionVersionBinding{}
	v, err := s.occurrenceInventory(ctx, who, tenant)
	if err != nil {
		return empty, false, err
	}
	for _, ref := range s.segmentParamRefs(v) {
		p, found, e := s.segmentOccurrenceParams(ctx, who, tenant, v, ref)
		if e != nil {
			return empty, false, e
		}
		if !found || p.CurrentLayers == nil || *p.CurrentLayers != node.Ref {
			continue
		}
		if node.ParentRef != ref && !(ref == s.paramsRef && node.ParentRef.ArtifactID == uuid.Nil) {
			continue
		}
		return p.Source, true, nil
	}
	if s.originalEntries[v2input.Entry{Ref: node.Ref, Contract: node.Contract}] {
		return empty, false, nil
	}
	return empty, false, v2input.ErrInvalid
}

func (s SQLAuthoredSegmentCatalog) maskGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
	empty := v2draft.DocumentGeometry{}
	v, err := s.occurrenceInventory(ctx, who, tenant)
	if err != nil {
		return empty, false, err
	}
	target := v2input.SessionVersionBinding{}
	requireProtected := false
	bind := func(b v2input.SessionVersionBinding, protected bool) error {
		if target.AssetVersionID != uuid.Nil && target != b {
			return v2input.ErrInvalid
		}
		target = b
		requireProtected = requireProtected || protected
		return nil
	}
	for _, ref := range s.segmentParamRefs(v) {
		p, found, e := s.segmentOccurrenceParams(ctx, who, tenant, v, ref)
		if e != nil {
			return empty, false, e
		}
		if !found {
			continue
		}
		if p.ProtectedMask != nil && *p.ProtectedMask == node.Ref && (node.ParentRef == ref || ref == s.paramsRef && node.ParentRef.ArtifactID == uuid.Nil) {
			if e = bind(p.Source, true); e != nil {
				return empty, false, e
			}
		}
		if p.CurrentLayers != nil && node.ParentRef == *p.CurrentLayers {
			raw, e := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, *p.CurrentLayers)
			if e != nil {
				return empty, false, e
			}
			children, e := v2draft.ValidateLayerStack(raw, p.Source)
			if e != nil {
				return empty, false, e
			}
			for _, child := range children {
				if child.Contract == "MaskRef" && child.Ref == node.Ref {
					if e = bind(p.Source, false); e != nil {
						return empty, false, e
					}
				}
			}
		}
	}
	// Canonical geometry results contain explicit transported MaskRef edges.
	// Only those exact typed edges may bind to the phase output canvas.
	for _, item := range v.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if phase.Kind != "geometry" || node.ParentRef != phase.Result {
			continue
		}
		raw, e := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, phase.Result)
		if e != nil {
			return empty, false, e
		}
		children, e := v2draft.DocumentChildren(raw, "AuthoredGeometryPhaseResult", phase.Result)
		if e != nil {
			return empty, false, e
		}
		for _, child := range children {
			if child.Contract == "MaskRef" && child.Ref == node.Ref {
				if e = bind(phase.OutputSource, false); e != nil {
					return empty, false, e
				}
			}
		}
	}
	// Exact canonical producer result/report parents retain their own output
	// canvas; an arbitrary retained JSON object is never a geometry grant.
	edge := func(parent v2input.Ref, contract string, binding v2input.SessionVersionBinding) error {
		if node.ParentRef != parent {
			return nil
		}
		raw, e := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, parent)
		if e != nil {
			return e
		}
		children, e := v2draft.DocumentChildren(raw, contract, parent)
		if e != nil {
			return e
		}
		for _, child := range children {
			if child.Contract == "MaskRef" && child.Ref == node.Ref {
				if e = bind(binding, false); e != nil {
					return e
				}
			}
		}
		return nil
	}
	for _, item := range v.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if phase.Kind == "segment" {
			if err = edge(phase.Result, "ImageResult", phase.OutputSource); err != nil {
				return empty, false, err
			}
		}
		if err = edge(phase.Report, "ImageReport", phase.OutputSource); err != nil {
			return empty, false, err
		}
		if phase.Geometry != nil {
			native := phase.Geometry
			raw, e := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, native.Image)
			if e != nil {
				return empty, false, e
			}
			image, e := review.ParseWorkingImage(raw)
			if e != nil {
				return empty, false, e
			}
			canvas, e := canonical.Hash(image.Canvas)
			if e != nil {
				return empty, false, e
			}
			binding := v2input.SessionVersionBinding{AssetVersionID: phase.InputSource.AssetVersionID, Source: native.Image, CanvasHash: canvas}
			if native.Task.Operation != "image.upscale" {
				if err = edge(native.Root, "ImageResult", binding); err != nil {
					return empty, false, err
				}
			}
			if err = edge(native.Report, "ImageReport", binding); err != nil {
				return empty, false, err
			}
			if node.ParentRef != phase.Result {
				raw, e = s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, phase.Result)
				if e != nil {
					return empty, false, e
				}
				var compound struct {
					BlendedResult v2input.Ref `json:"blended_result"`
				}
				if decodeProjection := json.Unmarshal(raw, &compound); decodeProjection != nil || !validLayerArtifactRef(compound.BlendedResult) {
					return empty, false, v2input.ErrInvalid
				}
				if err = edge(compound.BlendedResult, "ImageResult", phase.OutputSource); err != nil {
					return empty, false, err
				}
			}
		}
	}
	if node.ParentRef == v.BaseReceipt {
		var base struct {
			Coverage *v2input.Ref                  `json:"generated_coverage"`
			Source   v2input.SessionVersionBinding `json:"processed_source"`
		}
		if json.Unmarshal([]byte(v.BaseReceiptJCS), &base) != nil {
			return empty, false, v2input.ErrInvalid
		}
		if base.Coverage != nil && *base.Coverage == node.Ref {
			if err = bind(base.Source, false); err != nil {
				return empty, false, err
			}
		}
	}
	if target.AssetVersionID == uuid.Nil {
		if s.originalEntries[v2input.Entry{Ref: node.Ref, Contract: node.Contract}] {
			return empty, false, nil
		}
		return empty, false, v2input.ErrInvalid
	}
	raw, err := s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, node.Ref)
	if err != nil {
		return empty, false, err
	}
	mask, err := review.ParseMaskRef(raw)
	if err != nil || requireProtected && !mask.Protected {
		return empty, false, v2input.ErrInvalid
	}
	raw, err = s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, target.Source)
	if err != nil {
		return empty, false, err
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || mask.Canvas != image.Canvas || !bytes.Equal(mask.TransformChain, image.TransformChain) {
		return empty, false, v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != target.CanvasHash {
		return empty, false, v2input.ErrInvalid
	}
	chain, err := canonical.Hash(image.TransformChain)
	if err != nil {
		return empty, false, err
	}
	return v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}, true, nil
}
