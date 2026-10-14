package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Returns the original processing range after exact Params/source validation.
// It does not establish mask provenance, producer qualification or admission.
func DefectProposalDiameterFromParams(paramsRef ArtifactRef, raw []byte, source DefectSource) ([2]float64, error) {
	policy, err := DefectProposalInputPolicyFromParams(paramsRef, raw, source)
	if err != nil {
		return [2]float64{}, err
	}
	return policy.Diameter, nil
}

// Original mask locators and per-face bindings, owned by this decoder. These
// are content declarations, not protection/manual producer authority.
type DefectProposalInputPolicy struct {
	Diameter    [2]float64
	Permanent   *ProtectionMask
	Protections []ProtectionMask
	Faces       []DefectProposalFaceInput
}

func DefectProposalInputPolicyFromParams(paramsRef ArtifactRef, raw []byte, source DefectSource) (DefectProposalInputPolicy, error) {
	var zero DefectProposalInputPolicy
	if !paramsRef.valid() || len(raw) < 2 || len(raw) > 1048576 {
		return zero, errors.New("invalid original defect Params")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != paramsRef.SHA256 {
		return zero, errors.New("changed original defect Params")
	}
	var params proposalReportParams
	if decodeExact(raw, &params) != nil || !validProposalParams(params, source) {
		return zero, errors.New("invalid original defect Params")
	}
	result := DefectProposalInputPolicy{Diameter: [2]float64{params.Diameter[0], params.Diameter[1]}, Permanent: params.Permanent, Faces: params.Faces}
	if params.Protections != nil {
		result.Protections = *params.Protections
	}
	return result, nil
}
