package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// DefectProposalFaceInput is the original worker face binding. Catalog origin,
// qualification and landmark content remain preparation-service obligations.
type DefectProposalFaceInput = proposalFaceInput

// DefectProposalTaskInputs contains canonical Params and complete declared refs.
// It is not a queued task or a retained admission certificate.
type DefectProposalTaskInputs struct {
	Params       []byte
	ParamsSHA256 string
	Inputs       []ArtifactRef
	ManualRoots  []ArtifactRef
}

// BuildDefectProposalTaskInputs compiles the exact worker contract, preserving
// face order and explicitly emitting protections even when the list is empty.
func BuildDefectProposalTaskInputs(ctx context.Context, source DefectSource, faces []DefectProposalFaceInput, diameter [2]float64, permanent *ProtectionMask, additional []ProtectionMask, manuals []OriginalManualProtection) (DefectProposalTaskInputs, error) {
	empty := DefectProposalTaskInputs{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !source.Pixels.valid() || faces == nil || len(faces) > 64 {
		return empty, errors.New("invalid defect face inputs")
	}
	protections, err := BuildDefectProtectionInputs(ctx, source, permanent, additional, manuals)
	if err != nil {
		return empty, err
	}
	params := proposalReportParams{Image: source.Image, Faces: faces, Permanent: protections.Permanent, Protections: &protections.Protections, Diameter: []float64{diameter[0], diameter[1]}}
	if !validProposalParams(params, source) {
		return empty, errors.New("invalid defect proposal Params")
	}
	raw, err := canonical.Marshal(params)
	if err != nil {
		return empty, err
	}
	refs := map[uuid.UUID]ArtifactRef{}
	roles := map[uuid.UUID]string{}
	add := func(ref ArtifactRef, role string) error {
		if !ref.valid() {
			return errors.New("invalid defect task dependency")
		}
		if old, ok := refs[ref.ID]; ok && (old != ref || roles[ref.ID] != role) {
			return errors.New("conflicting defect dependency identity")
		}
		refs[ref.ID] = ref
		roles[ref.ID] = role
		return nil
	}
	if err = add(source.Image, "source"); err != nil {
		return empty, err
	}
	if err = add(source.Pixels, "source_pixels"); err != nil {
		return empty, err
	}
	for _, ref := range protections.Inputs {
		if err = add(ref, "protection"); err != nil {
			return empty, err
		}
	}
	for _, ref := range protections.Roots {
		if err = add(ref, "manual_root"); err != nil {
			return empty, err
		}
	}
	for _, face := range faces {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if err = add(face.Landmarks, "landmarks"); err != nil {
			return empty, err
		}
		for _, mask := range []*ProtectionMask{face.Skin, face.Manual} {
			if mask != nil {
				if err = add(mask.Artifact, "face_mask"); err != nil {
					return empty, err
				}
			}
		}
	}
	inputs := make([]ArtifactRef, 0, len(refs))
	for _, ref := range refs {
		inputs = append(inputs, ref)
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].ID.String() < inputs[j].ID.String() })
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	digest := sha256.Sum256(raw)
	return DefectProposalTaskInputs{Params: raw, ParamsSHA256: hex.EncodeToString(digest[:]), Inputs: inputs, ManualRoots: protections.Roots}, nil
}

// BuildDefectProposalTaskInputsFromImage binds the source geometry and RGB child
// to exact original WorkingImage bytes rather than independently supplied refs.
// Producer authority and qualified pixel/ICC verification remain caller duties.
func BuildDefectProposalTaskInputsFromImage(ctx context.Context, imageRef ArtifactRef, imageBody []byte, faces []DefectProposalFaceInput, diameter [2]float64, permanent *ProtectionMask, additional []ProtectionMask, manuals []OriginalManualProtection) (DefectProposalTaskInputs, error) {
	empty := DefectProposalTaskInputs{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !imageRef.valid() || len(imageBody) < 2 || len(imageBody) > 1048576 {
		return empty, errors.New("invalid original defect source")
	}
	digest := sha256.Sum256(imageBody)
	if hex.EncodeToString(digest[:]) != imageRef.SHA256 {
		return empty, errors.New("changed original defect source")
	}
	image, err := ParseWorkingImage(imageBody)
	if err != nil {
		return empty, err
	}
	if image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return empty, errors.New("unsupported defect source format")
	}
	compiled, err := BuildDefectProposalTaskInputs(ctx, image.Source(imageRef), faces, diameter, permanent, additional, manuals)
	if err != nil {
		return empty, err
	}
	var chain transformChain
	if err := json.Unmarshal(image.TransformChain, &chain); err != nil {
		return empty, err
	}
	refs := make(map[uuid.UUID]ArtifactRef, len(compiled.Inputs)+len(chain.Steps)+1)
	for _, ref := range compiled.Inputs {
		refs[ref.ID] = ref
	}
	add := func(ref ArtifactRef) error {
		if !ref.valid() || ref.ID == imageRef.ID || ref.ID == image.Pixels.ID {
			return errors.New("conflicting defect source array identity")
		}
		if old, exists := refs[ref.ID]; exists && old != ref {
			return errors.New("conflicting defect source array digest")
		}
		refs[ref.ID] = ref
		return nil
	}
	if image.Alpha != nil {
		if err := add(*image.Alpha); err != nil {
			return empty, err
		}
	}
	for _, step := range chain.Steps {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if step.InverseField != nil {
			if err := add(*step.InverseField); err != nil {
				return empty, err
			}
		}
	}
	if len(refs) > 4096 {
		return empty, errors.New("defect source dependency limit")
	}
	compiled.Inputs = make([]ArtifactRef, 0, len(refs))
	for _, ref := range refs {
		compiled.Inputs = append(compiled.Inputs, ref)
	}
	sort.Slice(compiled.Inputs, func(i, j int) bool { return compiled.Inputs[i].ID.String() < compiled.Inputs[j].ID.String() })
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return compiled, nil
}

// VerifyDefectProposalDeclaredInputs binds original Params bytes and requires
// every native worker input. Additional geometry/manual roots are permitted;
// authorization, scientific qualification and producer completeness are separate.
func VerifyDefectProposalDeclaredInputs(paramsRef ArtifactRef, raw []byte, source DefectSource, inputs []ArtifactRef) error {
	fail := func() error { return errors.New("incomplete original defect inputs") }
	if !paramsRef.valid() || len(raw) < 2 || len(raw) > 1048576 || !source.Pixels.valid() || len(inputs) == 0 || len(inputs) > 4096 {
		return fail()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != paramsRef.SHA256 {
		return fail()
	}
	var params proposalReportParams
	if decodeExact(raw, &params) != nil || !validProposalParams(params, source) {
		return fail()
	}
	declared := map[uuid.UUID]ArtifactRef{}
	for _, ref := range inputs {
		if !ref.valid() || ref.ID == paramsRef.ID {
			return fail()
		}
		if _, exists := declared[ref.ID]; exists {
			return fail()
		}
		declared[ref.ID] = ref
	}
	required := []ArtifactRef{source.Image, source.Pixels}
	if params.Permanent != nil {
		required = append(required, params.Permanent.Artifact)
	}
	if params.Protections != nil {
		for _, mask := range *params.Protections {
			required = append(required, mask.Artifact)
		}
	}
	for _, face := range params.Faces {
		required = append(required, face.Landmarks)
		if face.Skin != nil {
			required = append(required, face.Skin.Artifact)
		}
		if face.Manual != nil {
			required = append(required, face.Manual.Artifact)
		}
	}
	for _, ref := range required {
		if got, exists := declared[ref.ID]; !exists || got != ref {
			return fail()
		}
	}
	return nil
}

// DefectProposalMaskInputs returns fresh mask bindings from verified original
// Params. Physical arrays and catalog/producer authority remain caller duties.
func DefectProposalMaskInputs(paramsRef ArtifactRef, raw []byte, source DefectSource) ([]ProtectionMask, error) {
	if !paramsRef.valid() || len(raw) < 2 || len(raw) > 1048576 {
		return nil, errors.New("invalid original defect Params")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != paramsRef.SHA256 {
		return nil, errors.New("changed original defect Params")
	}
	var params proposalReportParams
	if decodeExact(raw, &params) != nil || !validProposalParams(params, source) {
		return nil, errors.New("invalid original defect Params")
	}
	masks := make([]ProtectionMask, 0)
	if params.Permanent != nil {
		masks = append(masks, *params.Permanent)
	}
	if params.Protections != nil {
		masks = append(masks, (*params.Protections)...)
	}
	for _, face := range params.Faces {
		if face.Skin != nil {
			masks = append(masks, *face.Skin)
		}
		if face.Manual != nil {
			masks = append(masks, *face.Manual)
		}
	}
	return masks, nil
}

// DefectProposalFaceInputsFromParams derives owned face bindings from original
// source-bound Params; producer qualification remains an independent obligation.
func DefectProposalFaceInputsFromParams(paramsRef ArtifactRef, raw []byte, source DefectSource) ([]DefectProposalFaceInput, error) {
	if !paramsRef.valid() || len(raw) < 2 || len(raw) > 1048576 {
		return nil, errors.New("invalid original defect Params")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != paramsRef.SHA256 {
		return nil, errors.New("changed original defect Params")
	}
	var params proposalReportParams
	if decodeExact(raw, &params) != nil || !validProposalParams(params, source) {
		return nil, errors.New("invalid original defect Params")
	}
	return params.Faces, nil
}

// VerifyDefectProposalFaceDocument validates the exact landmark bytes and selected
// object, including mask object bindings. Model claims are not qualification.
func VerifyDefectProposalFaceDocument(input DefectProposalFaceInput, raw []byte, source DefectSource) error {
	fail := func() error { return errors.New("invalid original defect face document") }
	if !input.Landmarks.valid() || len(raw) < 2 || len(raw) > 1048576 {
		return fail()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != input.Landmarks.SHA256 {
		return fail()
	}
	var face Face68
	var err error
	if input.ObjectID == nil {
		face, err = ParseFace68(raw, source)
	} else {
		face, err = ResolveFaceLandmarks(raw, source, *input.ObjectID)
	}
	if err != nil {
		return err
	}
	for i, mask := range []*ProtectionMask{input.Skin, input.Manual} {
		if mask == nil {
			continue
		}
		if !validMaskBinding(*mask) || mask.Protected || mask.Selector != []string{"skin", "manual"}[i] || mask.Canvas != source.Canvas || (mask.ObjectID != nil && *mask.ObjectID != face.ObjectID) {
			return fail()
		}
		maskChain, err := canonical.Bytes(mask.TransformChain)
		if err != nil {
			return fail()
		}
		sourceChain, err := canonical.Bytes(source.TransformChain)
		if err != nil || !bytes.Equal(maskChain, sourceChain) {
			return fail()
		}
	}
	return nil
}
