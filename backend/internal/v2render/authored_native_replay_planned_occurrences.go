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

// Only an actually adopted historical segment may authorize occurrence binding.
// The planned prefix is reread; no current-segment authority is relabelled.
func (s SQLAuthoredNativeReplayCatalog) plannedSegmentOccurrenceParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, inventory AuthoredFinalInventory, ref v2input.Ref) (AuthoredSegmentParams, bool, error) {
	empty := AuthoredSegmentParams{}

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
	if prior == nil {
		return empty, false, nil
	}
	raw, err := s.read(ctx, who, tenant, ref)
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
	if !authoredReplaySegmentOccurrenceMatches(p, *prior, inventory, tenant, s.request.Consumer.Parent) {
		return empty, false, v2input.ErrInvalid
	}

	return p, true, nil
}
func (s SQLAuthoredNativeReplayCatalog) plannedOccurrenceInventory(ctx context.Context, who auth.Identity, tenant uuid.UUID) (AuthoredFinalInventory, error) {
	return s.plannedInventory(ctx, who, tenant)
}

func (s SQLAuthoredNativeReplayCatalog) plannedSegmentParamRefs(v AuthoredFinalInventory) []v2input.Ref {
	refs := []v2input.Ref{}
	for _, item := range v.Phases {
		var phase AuthoredPhaseReceipt
		if decodeAuthoredParams([]byte(item.ReceiptJCS), &phase) == nil && phase.Kind == "segment" {
			refs = append(refs, phase.Params)
		}
	}
	return refs
}
func (s SQLAuthoredNativeReplayCatalog) plannedLayerStackBinding(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2input.SessionVersionBinding, bool, error) {
	empty := v2input.SessionVersionBinding{}
	v, err := s.plannedOccurrenceInventory(ctx, who, tenant)
	if err != nil {
		return empty, false, err
	}
	for _, ref := range s.plannedSegmentParamRefs(v) {
		p, found, e := s.plannedSegmentOccurrenceParams(ctx, who, tenant, v, ref)
		if e != nil {
			return empty, false, e
		}
		if !found || p.CurrentLayers == nil || *p.CurrentLayers != node.Ref {
			continue
		}
		if node.ParentRef != ref {
			continue
		}
		return p.Source, true, nil
	}
	if s.originalEntries[v2input.Entry{Ref: node.Ref, Contract: node.Contract}] {
		return empty, false, nil
	}
	return empty, false, v2input.ErrInvalid
}

func (s SQLAuthoredNativeReplayCatalog) plannedMaskGeometryContent(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
	empty := v2draft.DocumentGeometry{}
	v, err := s.plannedOccurrenceInventory(ctx, who, tenant)
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
	for _, ref := range s.plannedSegmentParamRefs(v) {
		p, found, e := s.plannedSegmentOccurrenceParams(ctx, who, tenant, v, ref)
		if e != nil {
			return empty, false, e
		}
		if !found {
			continue
		}
		if p.ProtectedMask != nil && *p.ProtectedMask == node.Ref && node.ParentRef == ref {
			if e = bind(p.Source, true); e != nil {
				return empty, false, e
			}
		}
		if p.CurrentLayers != nil && node.ParentRef == *p.CurrentLayers {
			raw, e := s.read(ctx, who, tenant, *p.CurrentLayers)
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
		raw, e := s.read(ctx, who, tenant, phase.Result)
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
		raw, e := s.read(ctx, who, tenant, parent)
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
			// A native proposal keeps its operation-specific geometry, which
			// may differ from the final blend target. Recheck the actual root.
			expected, e := s.nativeGeometry(ctx, who, tenant, phase)
			if e != nil {
				return empty, false, e
			}
			native := phase.Geometry
			raw, e := s.read(ctx, who, tenant, native.Image)
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
			chain, e := canonical.Hash(image.TransformChain)
			if e != nil || canvas != expected.CanvasHash || chain != expected.TransformHash {
				return empty, false, v2input.ErrInvalid
			}
			binding := v2input.SessionVersionBinding{AssetVersionID: phase.InputSource.AssetVersionID, Source: native.Image, CanvasHash: canvas}
			if native.Task.Operation != "image.upscale" {
				if err = edge(native.Root, "ImageResult", binding); err != nil {
					return empty, false, err
				}
			}
			reportContract := "ImageReport"
			if native.Task.Operation == "image.upscale" {
				reportContract = "UpscaleReport"
			}
			if err = edge(native.Report, reportContract, binding); err != nil {
				return empty, false, err
			}
			if node.ParentRef != phase.Result {
				raw, e = s.read(ctx, who, tenant, phase.Result)
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
	raw, err := s.read(ctx, who, tenant, node.Ref)
	if err != nil {
		return empty, false, err
	}
	mask, err := review.ParseMaskRef(raw)
	if err != nil || requireProtected && !mask.Protected {
		return empty, false, v2input.ErrInvalid
	}
	raw, err = s.read(ctx, who, tenant, target.Source)
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

// Pure identity proof over a SQL-selected planned prefix; it grants no artifact IO.
func authoredReplaySegmentOccurrenceMatches(p AuthoredSegmentParams, phase AuthoredPhaseReceipt, inventory AuthoredFinalInventory, tenant, parent uuid.UUID) bool {
	if phase.Kind != "segment" || phase.Tenant != tenant || phase.Parent != parent || phase.Ordinal < 1 {
		return false
	}
	if p.TenantID != tenant || p.ParentRunID != parent || p.WorkflowID != inventory.Context.Workflow || p.ProgramSHA256 != inventory.Context.ProgramSHA || p.Recipe != inventory.Context.OriginalRecipe || p.BaseReceipt != inventory.BaseReceipt {
		return false
	}
	if p.Ordinal != phase.Ordinal || p.InstructionID != phase.Instruction || p.Source != phase.InputSource || p.PredecessorSHA256 != phase.PredecessorSHA {
		return false
	}
	if len(p.PhaseReceipts) != phase.Ordinal-1 || len(p.PhaseReceipts) > len(inventory.Phases) {
		return false
	}
	for i, ref := range p.PhaseReceipts {
		if ref != inventory.Phases[i].Receipt {
			return false
		}
	}
	return true
}

// Revalidate the complete planned prefix after physical occurrence reads.
func (s SQLAuthoredNativeReplayCatalog) LayerStackBinding(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2input.SessionVersionBinding, bool, error) {
	before, err := s.plannedInventory(ctx, who, tenant)
	if err != nil {
		return v2input.SessionVersionBinding{}, false, err
	}
	binding, found, err := s.plannedLayerStackBinding(ctx, who, tenant, node)
	if err != nil {
		return v2input.SessionVersionBinding{}, false, err
	}
	if err = s.plannedPrefixUnchanged(ctx, who, tenant, before); err != nil {
		return v2input.SessionVersionBinding{}, false, err
	}
	return binding, found, nil
}
func (s SQLAuthoredNativeReplayCatalog) plannedMaskGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
	before, err := s.plannedInventory(ctx, who, tenant)
	if err != nil {
		return v2draft.DocumentGeometry{}, false, err
	}
	geometry, found, err := s.plannedMaskGeometryContent(ctx, who, tenant, node)
	if err != nil {
		return v2draft.DocumentGeometry{}, false, err
	}
	if err = s.plannedPrefixUnchanged(ctx, who, tenant, before); err != nil {
		return v2draft.DocumentGeometry{}, false, err
	}
	return geometry, found, nil
}
func (s SQLAuthoredNativeReplayCatalog) plannedPrefixUnchanged(ctx context.Context, who auth.Identity, tenant uuid.UUID, before AuthoredFinalInventory) error {
	after, err := s.plannedInventory(ctx, who, tenant)
	if err != nil {
		return err
	}
	left, err := canonical.Marshal(before)
	if err != nil {
		return err
	}
	right, err := canonical.Marshal(after)
	if err != nil {
		return err
	}
	if !bytes.Equal(left, right) {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
