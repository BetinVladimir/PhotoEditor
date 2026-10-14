package review

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"sort"
)

// OriginalManualProtection carries original catalog-authorized manifest bytes.
// The caller must independently prove producer/source authority before compiling.
type OriginalManualProtection struct {
	// Raster is a validated immutable vector specification, not a pixel proof.
	Raster ManualRasterPlan
	Root   ArtifactRef
	Body   []byte
	Target ProtectionMask
}

// DefectProtectionInputs is the protection component of worker Params/inputs.
// Roots identify retained original dependencies, not an admission certificate.
type DefectProtectionInputs struct {
	Permanent   *ProtectionMask
	Protections []ProtectionMask
	Inputs      []ArtifactRef
	Roots       []ArtifactRef
}

// BuildDefectProtectionInputs automatically derives every manual protected child
// from original roots and combines it with server-selected protection policy.
// It never trusts a caller-selected protected child or silently truncates masks.
func BuildDefectProtectionInputs(ctx context.Context, source DefectSource, permanent *ProtectionMask, additional []ProtectionMask, manuals []OriginalManualProtection) (DefectProtectionInputs, error) {
	empty := DefectProtectionInputs{}
	fail := func() (DefectProtectionInputs, error) { return empty, errors.New("invalid defect protection inputs") }
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !source.Image.valid() || !validCanvas(source.Canvas) || validateTransformChain(source.TransformChain, source.Canvas) != nil || len(additional) > 64 || len(manuals) > 64 {
		return fail()
	}
	if len(manuals) > 0 {
		var geometry transformChain
		if decodeExact(source.TransformChain, &geometry) != nil || len(geometry.Steps) != 0 {
			return fail()
		}
	}
	chain, err := canonical.Bytes(source.TransformChain)
	if err != nil {
		return fail()
	}
	masks := map[uuid.UUID]ProtectionMask{}
	bodies := map[uuid.UUID][]byte{}
	roots := map[uuid.UUID]ArtifactRef{}
	// An artifact identity has one binary/document role in the compiled graph.
	roles := map[uuid.UUID]string{source.Image.ID: "source"}
	result := DefectProtectionInputs{Protections: make([]ProtectionMask, 0), Inputs: make([]ArtifactRef, 0), Roots: make([]ArtifactRef, 0)}
	add := func(mask ProtectionMask) (ProtectionMask, error) {
		if role, exists := roles[mask.Artifact.ID]; exists && role != "protection" {
			return ProtectionMask{}, errors.New("conflicting protection artifact role")
		}
		if !mask.Protected || !validMaskBinding(mask) || mask.Canvas != source.Canvas {
			return ProtectionMask{}, errors.New("invalid required mask")
		}
		current, err := canonical.Bytes(mask.TransformChain)
		if err != nil || !bytes.Equal(chain, current) {
			return ProtectionMask{}, errors.New("protection geometry mismatch")
		}
		body, err := canonical.Marshal(mask)
		if err != nil {
			return ProtectionMask{}, err
		}
		if old, ok := bodies[mask.Artifact.ID]; ok && !bytes.Equal(old, body) {
			return ProtectionMask{}, errors.New("conflicting protection identity")
		}
		var owned ProtectionMask
		if err := decodeExact(body, &owned); err != nil {
			return ProtectionMask{}, err
		}
		roles[owned.Artifact.ID] = "protection"
		bodies[owned.Artifact.ID] = body
		masks[owned.Artifact.ID] = owned
		return owned, nil
	}
	if permanent != nil {
		owned, err := add(*permanent)
		if err != nil {
			return empty, err
		}
		result.Permanent = &owned
	}
	for _, mask := range additional {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if _, err := add(mask); err != nil {
			return empty, err
		}
	}
	metadata := 0
	for _, manual := range manuals {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if len(manual.Body) > 4194304-metadata {
			return fail()
		}
		metadata += len(manual.Body)
		if role, exists := roles[manual.Root.ID]; exists && role != "manual_root" {
			return fail()
		}
		roles[manual.Root.ID] = "manual_root"
		inventory, err := ResolveManualFaceMaskOutputs(manual.Body, manual.Root, manual.Target, source)
		if err != nil {
			return empty, err
		}
		if previous, ok := roots[manual.Root.ID]; ok && previous != manual.Root {
			return fail()
		}
		roots[manual.Root.ID] = manual.Root
		if _, err := add(inventory.Protected); err != nil {
			return empty, err
		}
	}
	for id, mask := range masks {
		result.Inputs = append(result.Inputs, mask.Artifact)
		if result.Permanent == nil || id != result.Permanent.Artifact.ID {
			result.Protections = append(result.Protections, mask)
		}
	}
	if len(result.Protections) > 64 {
		return fail()
	}
	for _, root := range roots {
		result.Roots = append(result.Roots, root)
	}
	sort.Slice(result.Protections, func(i, j int) bool {
		return result.Protections[i].Artifact.ID.String() < result.Protections[j].Artifact.ID.String()
	})
	sort.Slice(result.Inputs, func(i, j int) bool { return result.Inputs[i].ID.String() < result.Inputs[j].ID.String() })
	sort.Slice(result.Roots, func(i, j int) bool { return result.Roots[i].ID.String() < result.Roots[j].ID.String() })
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
