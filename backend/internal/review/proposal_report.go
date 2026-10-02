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
	Landmarks  ArtifactRef     `json:"landmarks"`
	Skin       *ProtectionMask `json:"skin"`
	Manual     *ProtectionMask `json:"manual"`
	ManualMode bool            `json:"manual_mode"`
}
type proposalReportParams struct {
	Image     ArtifactRef         `json:"image"`
	Faces     []proposalFaceInput `json:"faces"`
	Permanent *ProtectionMask     `json:"permanent"`
	Diameter  []float64           `json:"diameter_face_fraction"`
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
	if p.Faces == nil || len(p.Faces) > 64 || len(p.Diameter) != 2 || p.Diameter[0] < .002 || p.Diameter[0] > p.Diameter[1] || p.Diameter[1] > .012 {
		return fail()
	}
	if p.Permanent != nil && (!p.Permanent.Protected || !validMaskBinding(*p.Permanent)) {
		return fail()
	}
	seen := map[uuid.UUID]bool{}
	for _, f := range p.Faces {
		if !f.Landmarks.valid() || seen[f.Landmarks.ID] || (f.ManualMode && f.Manual == nil) || (!f.ManualMode && f.Skin == nil) {
			return fail()
		}
		seen[f.Landmarks.ID] = true
		if f.Skin != nil && (!validMaskBinding(*f.Skin) || f.Skin.Selector != "skin" || f.Skin.Protected) {
			return fail()
		}
		if f.Manual != nil && (!validMaskBinding(*f.Manual) || f.Manual.Selector != "manual" || f.Manual.Protected) {
			return fail()
		}
	}
	if d.Faces == nil || len(d.Faces) > 64 || d.Warnings == nil || len(d.Warnings) > 3 {
		return fail()
	}
	for _, f := range d.Faces {
		if f.ObjectID == uuid.Nil || f.Threshold < 0 || f.Candidates < 0 || f.Candidates > 4096 || f.Overlapping < 0 || f.Overlapping > 4096 {
			return fail()
		}
		if f.Reason != nil {
			switch *f.Reason {
			case "low_confidence", "face_too_small", "empty_skin", "subpixel_diameter", "degenerate_landmarks", "incomplete_face_roi":
			default:
				return fail()
			}
		}
	}
	for _, w := range d.Warnings {
		switch w {
		case "no_faces", "face_skipped", "overlapping_candidates":
		default:
			return fail()
		}
	}
	return d.Protection, nil
}
