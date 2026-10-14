package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
)

// ResolveProposalFace binds one original landmark document to a validated
// defect report's Params face. Caller proves report/document catalog origin and
// qualification separately; this function never grants processing admission.
func ResolveProposalFace(reportRaw []byte, proposal ArtifactRef, source DefectSource, ordinal int, landmarkRef ArtifactRef, landmarkRaw []byte) (Face68, error) {
	fail := func() (Face68, error) { return Face68{}, errors.New("invalid defect face evidence") }
	if _, err := ParseProposalProtection(reportRaw, proposal, source); err != nil {
		return fail()
	}
	var report proposalReportDocument
	if decodeExact(reportRaw, &report) != nil || ordinal < 0 || ordinal >= len(report.Params.Faces) || !landmarkRef.valid() || len(landmarkRaw) < 2 || len(landmarkRaw) > 1048576 {
		return fail()
	}
	binding := report.Params.Faces[ordinal]
	digest := sha256.Sum256(landmarkRaw)
	if binding.Landmarks != landmarkRef || hex.EncodeToString(digest[:]) != landmarkRef.SHA256 {
		return fail()
	}
	var face Face68
	var err error
	if binding.ObjectID == nil {
		face, err = ParseFace68(landmarkRaw, source)
	} else {
		face, err = ResolveFaceLandmarks(landmarkRaw, source, *binding.ObjectID)
	}
	if err != nil {
		return fail()
	}
	for _, mask := range []*ProtectionMask{binding.Skin, binding.Manual} {
		if mask != nil && mask.ObjectID != nil && *mask.ObjectID != face.ObjectID {
			return fail()
		}
	}
	found := false
	for _, entry := range report.Faces {
		if entry.ObjectID == face.ObjectID {
			found = true
		}
	}
	if !found {
		return fail()
	}
	return face, nil
}

// ResolveProposalFaces validates the complete face set from original documents.
// The loader must authorize and pin each returned object independently. Document
// bytes are processed one at a time; successful faces are owned by the result.
// Any missing, repeated or unreported resolved object yields no partial result.
func ResolveProposalFaces(ctx context.Context, reportRaw []byte, proposal ArtifactRef, source DefectSource, load func(context.Context, ArtifactRef) ([]byte, error)) ([]Face68, error) {
	if len(reportRaw) > 1048576 || len(source.TransformChain) > 1048576 {
		return nil, errors.New("face evidence exceeds byte limit")
	}
	reportRaw = append([]byte(nil), reportRaw...)
	source.TransformChain = append([]byte(nil), source.TransformChain...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := ParseProposalProtection(reportRaw, proposal, source); err != nil {
		return nil, err
	}
	var report proposalReportDocument
	if err := decodeExact(reportRaw, &report); err != nil {
		return nil, err
	}
	if len(report.Params.Faces) > 0 && load == nil {
		return nil, errors.New("missing landmark loader")
	}
	result := make([]Face68, 0, len(report.Params.Faces))
	seen := map[uuid.UUID]bool{}
	for i, binding := range report.Params.Faces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := load(ctx, binding.Landmarks)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		face, err := ResolveProposalFace(reportRaw, proposal, source, i, binding.Landmarks, raw)
		if err != nil {
			return nil, err
		}
		if seen[face.ObjectID] {
			return nil, errors.New("duplicate resolved defect face")
		}
		seen[face.ObjectID] = true
		result = append(result, face)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ProposalLandmarkRefs derives the complete unique dependency list from a
// source-bound validated report, in first-use order. Different digests for one
// artifact ID are contradictory and fail before any catalog/object IO.
func ProposalLandmarkRefs(reportRaw []byte, proposal ArtifactRef, source DefectSource) ([]ArtifactRef, error) {
	if _, err := ParseProposalProtection(reportRaw, proposal, source); err != nil {
		return nil, err
	}
	var report proposalReportDocument
	if err := decodeExact(reportRaw, &report); err != nil {
		return nil, err
	}
	refs := make([]ArtifactRef, 0, len(report.Params.Faces))
	seen := map[uuid.UUID]string{}
	for _, face := range report.Params.Faces {
		if digest, exists := seen[face.Landmarks.ID]; exists {
			if digest != face.Landmarks.SHA256 {
				return nil, errors.New("conflicting landmark artifact digests")
			}
			continue
		}
		seen[face.Landmarks.ID] = face.Landmarks.SHA256
		refs = append(refs, face.Landmarks)
	}
	return refs, nil
}
