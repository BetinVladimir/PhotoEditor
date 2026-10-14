package review

import (
	"bytes"
	"errors"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

type proposalFaceInput struct {
	ObjectID   *uuid.UUID      `json:"object_id"`
	Landmarks  ArtifactRef     `json:"landmarks"`
	Skin       *ProtectionMask `json:"skin"`
	Manual     *ProtectionMask `json:"manual"`
	ManualMode bool            `json:"manual_mode"`
}
type proposalReportParams struct {
	Protections *[]ProtectionMask   `json:"protections,omitempty"`
	Image       ArtifactRef         `json:"image"`
	Faces       []proposalFaceInput `json:"faces"`
	Permanent   *ProtectionMask     `json:"permanent"`
	Diameter    []float64           `json:"diameter_face_fraction"`
}
type proposalFaceEvidence struct {
	ObjectID    uuid.UUID `json:"object_id"`
	Reason      *string   `json:"reason"`
	Threshold   float64   `json:"threshold"`
	Candidates  int       `json:"candidates"`
	Overlapping int       `json:"overlapping_candidates"`
}
type proposalReportDocument struct {
	Params     proposalReportParams   `json:"params"`
	Proposal   ArtifactRef            `json:"proposal"`
	Protection ProtectionMask         `json:"protection"`
	LabelPNG   ArtifactRef            `json:"label_png"`
	Faces      []proposalFaceEvidence `json:"faces"`
	Warnings   []string               `json:"warnings"`
	Algorithm  string                 `json:"algorithm"`
}

func validMaskBinding(mask ProtectionMask) bool {
	if !mask.Artifact.valid() || !validCanvas(mask.Canvas) || !spaceIDPattern.MatchString(mask.Selector) || math.IsNaN(mask.Confidence) || math.IsInf(mask.Confidence, 0) || mask.Confidence < 0 || mask.Confidence > 1 || validateTransformChain(mask.TransformChain, mask.Canvas) != nil {
		return false
	}
	if mask.ObjectID != nil && *mask.ObjectID == uuid.Nil {
		return false
	}
	if mask.ModelLockID != nil {
		if _, err := domain.ParseDigest(*mask.ModelLockID); err != nil {
			return false
		}
	}
	return true
}

// ParseProposalProtection accepts the canonical producer report and binds its
// protection to the exact proposal/source geometry. Storage/task provenance is
// a separate caller obligation; this function never authorizes a review.
func ParseProposalProtection(raw []byte, proposal ArtifactRef, source DefectSource) (ProtectionMask, error) {
	fail := func() (ProtectionMask, error) {
		return ProtectionMask{}, errors.New("invalid proposal protection binding")
	}
	var d proposalReportDocument
	if decodeExact(raw, &d) != nil || !proposal.valid() || !source.Image.valid() || d.Algorithm != "defect_dog_v1" || d.Proposal != proposal || d.Params.Image != source.Image || !d.LabelPNG.valid() || !d.Protection.Protected || !validMaskBinding(d.Protection) || d.Protection.Canvas != source.Canvas {
		return fail()
	}
	a, err := canonical.Bytes(d.Protection.TransformChain)
	if err != nil {
		return fail()
	}
	b, err := canonical.Bytes(source.TransformChain)
	if err != nil || !bytes.Equal(a, b) {
		return fail()
	}
	p := d.Params
	if !validProposalParams(p, source) {
		return fail()
	}
	if d.Faces == nil || len(d.Faces) != len(p.Faces) || len(d.Faces) > 64 || d.Warnings == nil || len(d.Warnings) > 3 {
		return fail()
	}
	reportedObjects := map[uuid.UUID]bool{}
	skipped, overlapping := false, false
	for _, f := range d.Faces {
		if f.ObjectID == uuid.Nil || reportedObjects[f.ObjectID] || f.Threshold < 0 || f.Candidates < 0 || f.Candidates > 4096 || f.Overlapping < 0 || f.Overlapping > 4096 {
			return fail()
		}
		reportedObjects[f.ObjectID] = true
		overlapping = overlapping || f.Overlapping > 0
		if f.Reason != nil {
			skipped = true
			switch *f.Reason {
			case "low_confidence", "face_too_small", "empty_skin", "subpixel_diameter", "degenerate_landmarks", "incomplete_face_roi":
			default:
				return fail()
			}
		}
	}
	for _, face := range p.Faces {
		if face.ObjectID != nil && !reportedObjects[*face.ObjectID] {
			return fail()
		}
	}
	warningSet := map[string]bool{}
	for _, w := range d.Warnings {
		if warningSet[w] {
			return fail()
		}
		warningSet[w] = true
		switch w {
		case "no_faces", "face_skipped", "overlapping_candidates":
		default:
			return fail()
		}
	}
	if warningSet["no_faces"] != (len(p.Faces) == 0) || warningSet["face_skipped"] != skipped || warningSet["overlapping_candidates"] != overlapping {
		return fail()
	}
	return d.Protection, nil
}

// ProposalLabelPNG derives the original preview ref only after full report validation.
func ProposalLabelPNG(raw []byte, proposal ArtifactRef, source DefectSource) (ArtifactRef, error) {
	if _, err := ParseProposalProtection(raw, proposal, source); err != nil {
		return ArtifactRef{}, err
	}
	var document proposalReportDocument
	if err := decodeExact(raw, &document); err != nil {
		return ArtifactRef{}, err
	}
	return document.LabelPNG, nil
}

// ProposalDeclaredProtections returns owned original permanent/additional masks.
// Source binding is validated; catalog authority and policy remain separate.
func ProposalDeclaredProtections(raw []byte, proposal ArtifactRef, source DefectSource) ([]ProtectionMask, error) {
	if _, err := ParseProposalProtection(raw, proposal, source); err != nil {
		return nil, err
	}
	var document proposalReportDocument
	if err := decodeExact(raw, &document); err != nil {
		return nil, err
	}
	result := make([]ProtectionMask, 0, 65)
	if document.Params.Permanent != nil {
		result = append(result, *document.Params.Permanent)
	}
	if document.Params.Protections != nil {
		result = append(result, (*document.Params.Protections)...)
	}
	return result, nil
}

// Shared by task compilation and original report validation.
func validProposalParams(p proposalReportParams, source DefectSource) bool {
	if !source.Image.valid() || p.Image != source.Image || !validCanvas(source.Canvas) || validateTransformChain(source.TransformChain, source.Canvas) != nil {
		return false
	}
	b, err := canonical.Bytes(source.TransformChain)
	if err != nil {
		return false
	}
	sourceMask := func(mask ProtectionMask) bool {
		if mask.Canvas != source.Canvas {
			return false
		}
		a, err := canonical.Bytes(mask.TransformChain)
		return err == nil && bytes.Equal(a, b)
	}
	if p.Faces == nil || len(p.Faces) > 64 || len(p.Diameter) != 2 || p.Diameter[0] < .002 || p.Diameter[0] > p.Diameter[1] || p.Diameter[1] > .012 {
		return false
	}
	protections := []ProtectionMask(nil)
	if p.Protections != nil {
		protections = *p.Protections
	}
	if len(protections) > 64 {
		return false
	}
	seenProtection := map[uuid.UUID]bool{}
	if p.Permanent != nil {
		seenProtection[p.Permanent.Artifact.ID] = true
	}
	for _, mask := range protections {
		if !mask.Protected || !validMaskBinding(mask) || !sourceMask(mask) || seenProtection[mask.Artifact.ID] {
			return false
		}
		seenProtection[mask.Artifact.ID] = true
	}
	if p.Permanent != nil && (!p.Permanent.Protected || !validMaskBinding(*p.Permanent) || !sourceMask(*p.Permanent)) {
		return false
	}
	type faceKey struct {
		landmarks uuid.UUID
		object    uuid.UUID
	}
	seen := map[faceKey]bool{}
	explicitObjects := map[uuid.UUID]bool{}
	for _, f := range p.Faces {
		key := faceKey{landmarks: f.Landmarks.ID}
		if f.ObjectID != nil {
			if *f.ObjectID == uuid.Nil {
				return false
			}
			if explicitObjects[*f.ObjectID] {
				return false
			}
			explicitObjects[*f.ObjectID] = true
			key.object = *f.ObjectID
		}
		if !f.Landmarks.valid() || seen[key] || (f.ManualMode && f.Manual == nil) || (!f.ManualMode && f.Skin == nil) {
			return false
		}
		seen[key] = true
		if f.Skin != nil && (!validMaskBinding(*f.Skin) || !sourceMask(*f.Skin) || f.Skin.Selector != "skin" || f.Skin.Protected) {
			return false
		}
		if f.Manual != nil && (!validMaskBinding(*f.Manual) || !sourceMask(*f.Manual) || f.Manual.Selector != "manual" || f.Manual.Protected) {
			return false
		}
		for _, mask := range []*ProtectionMask{f.Skin, f.Manual} {
			if mask != nil && f.ObjectID != nil && mask.ObjectID != nil && *mask.ObjectID != *f.ObjectID {
				return false
			}
		}
	}
	return true
}
