package semanticproposal

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Reconstructs typed file descriptors from original verified content. No caller
// descriptor inventory is accepted. This is preparation only; execution-policy
// qualification and retained transaction admission remain mandatory.
func (s SQLTransferSourceReader) prepareOriginDefectDescriptors(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, origin DefectOriginPins) (defectDescriptorSnapshot, error) {
	var zero defectDescriptorSnapshot
	published, prepared, err := s.prepareOriginDefectDependencies(ctx, who, tenantID, binding, color, origin)
	if err != nil {
		return zero, err
	}
	pins, err := decodeSourcePreparationPins(prepared.source)
	if err != nil {
		return zero, err
	}
	imageBody, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	paramsRef := review.ArtifactRef{ID: published.Ref.ArtifactID, SHA256: published.Ref.SHA256}
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	faces, err := review.DefectProposalFaceInputsFromParams(paramsRef, []byte(prepared.params.params), source)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	masks, err := review.DefectProposalMaskInputs(paramsRef, []byte(prepared.params.params), source)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	roots, err := s.ReadDefectFaceMaskRoots(ctx, who, tenantID, binding, origin)
	if err != nil {
		return zero, err
	}
	parents := map[v2input.Ref]v2input.Ref{}
	bindParent := func(child, root v2input.Ref) error {
		if old, ok := parents[child]; ok && old != root {
			return v2input.ErrConflict
		}
		parents[child] = root
		return nil
	}
	for _, root := range roots {
		if err := bindParent(v2input.Ref{ArtifactID: root.Binding.Mask.Artifact.ID, SHA256: root.Binding.Mask.Artifact.SHA256}, v2input.Ref{ArtifactID: root.Root.ID, SHA256: root.Root.SHA256}); err != nil {
			return zero, err
		}
	}
	for i := 0; i < prepared.manualCount; i++ {
		proof := prepared.manual[i].original()
		for _, child := range []v2input.Ref{proof.objects.Pixel.ref, proof.children[0].ref} {
			if err := bindParent(child, proof.objects.Root.ref); err != nil {
				return zero, err
			}
		}
	}
	selected := map[uuid.UUID]v2draft.LayerRef{}
	add := func(node v2draft.LayerRef) error {
		if node.Ref.ArtifactID == uuid.Nil {
			return v2input.ErrInvalid
		}
		if old, ok := selected[node.Ref.ArtifactID]; ok {
			a, e1 := canonical.Marshal(old)
			b, e2 := canonical.Marshal(node)
			if e1 != nil || e2 != nil || string(a) != string(b) {
				return v2input.ErrConflict
			}
			return nil
		}
		if len(selected) >= 4097 {
			return v2input.ErrInvalid
		}
		selected[node.Ref.ArtifactID] = node
		return nil
	}
	if err := add(v2draft.LayerRef{Ref: published.Ref, Contract: "Params"}); err != nil {
		return zero, err
	}
	for _, pin := range pins {
		if err := add(pin.Node); err != nil {
			return zero, err
		}
	}
	for _, face := range faces {
		if err := add(v2draft.LayerRef{Ref: v2input.Ref{ArtifactID: face.Landmarks.ID, SHA256: face.Landmarks.SHA256}, Contract: "FaceLandmarks", CanvasHash: binding.CanvasHash}); err != nil {
			return zero, err
		}
	}
	for _, mask := range masks {
		child := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
		root, ok := parents[child]
		if !ok {
			protection, err := s.readDiscoveredProtection(ctx, who, tenantID, binding, child)
			if err != nil {
				return zero, err
			}
			root = protection.Root
		}
		if err := add(v2draft.LayerRef{Ref: root, Contract: "MaskRef", CanvasHash: binding.CanvasHash}); err != nil {
			return zero, err
		}
		if err := add(v2draft.LayerRef{Ref: child, Contract: "MaskPixels", ParentRef: root}); err != nil {
			return zero, err
		}
	}
	// Every original compiler dependency must have an explicit typed descriptor;
	// never guess Artifact for an unknown role or silently drop an input.
	for _, ref := range published.Inputs {
		node, ok := selected[ref.ID]
		if !ok || node.Ref.SHA256 != ref.SHA256 {
			return zero, v2input.ErrInvalid
		}
	}
	published.Inputs = make([]review.ArtifactRef, 0, len(selected)-1)
	for id, node := range selected {
		if id != published.Ref.ArtifactID {
			published.Inputs = append(published.Inputs, review.ArtifactRef{ID: id, SHA256: node.Ref.SHA256})
		}
	}
	sort.Slice(published.Inputs, func(i, j int) bool { return published.Inputs[i].ID.String() < published.Inputs[j].ID.String() })
	nodes := make([]v2draft.LayerRef, 0, len(selected))
	nodes = append(nodes, selected[published.Ref.ArtifactID])
	for _, ref := range published.Inputs {
		nodes = append(nodes, selected[ref.ID])
	}
	snapshot, err := snapshotDefectDescriptors(published, nodes)
	if err != nil {
		return zero, err
	}
	if err := s.recheckManualDefectParams(ctx, who, tenantID, binding, color, prepared); err != nil {
		return zero, err
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, origin); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return snapshot, nil
}

// Uses server-reconstructed dependencies and server-selected qualification
// reports. Original producer pins and measured resource policy remain inputs;
// public prerequisite discovery and parent workflow admission are separate.
func (s SQLTransferSourceReader) prepareOriginSelectedDefectExecution(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, origin DefectOriginPins, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (defectPolicyExecutionPreparation, error) {
	var zero defectPolicyExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	// Pure minimum-size lower bound rejects impossible installed budgets before
	// catalog IO. Complete dimensions and file accounting are checked below.
	if _, err := v2input.CompileSemanticDefectProfile(8, 8, 1, costs, ram, vram, scratch, output); err != nil {
		return zero, err
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, tenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(policy, []string{"defects.propose", "face.landmarks", "face.detect", "face.parse"})
	if err != nil {
		return zero, err
	}
	report, err := s.discoverDefectQualificationReport(ctx, who, tenantID, selected["defects.propose"])
	if err != nil {
		return zero, err
	}
	if _, err := s.readDefectExecutionQualification(ctx, who, tenantID, report, selected["defects.propose"], costs); err != nil {
		return zero, err
	}
	image, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return zero, err
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, image)
	if err != nil {
		return zero, err
	}
	// Source JSON alone is a conservative byte lower bound, not a complete file
	// estimate. The catalog-backed qualification path still measures all inputs.
	if _, err := v2input.CompileSemanticDefectProfile(metadata.Width, metadata.Height, int64(len(image)), costs, ram, vram, scratch, output); err != nil {
		return zero, err
	}
	descriptors, err := s.prepareOriginDefectDescriptors(ctx, who, tenantID, binding, color, origin)
	if err != nil {
		return zero, fmt.Errorf("reconstruct original defect descriptors: %w", err)
	}
	prepared, err := s.prepareDiscoveredDefectExecution(ctx, who, tenantID, binding, color, origin, descriptors.Published, descriptors.Nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, fmt.Errorf("qualify original defect execution: %w", err)
	}
	if prepared.policy != policy {
		return zero, v2input.ErrConflict
	}
	return prepared, nil
}
