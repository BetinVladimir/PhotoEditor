package review

import (
	"errors"
	"github.com/google/uuid"
)

// ProposalFaceMask retains each declared mask binding, including masks that do
// not select the current mode. Producer confidence can depend on both masks.
// ObjectID remains nullable for legacy standalone landmark documents.
type ProposalFaceMask struct {
	Ordinal    int
	ObjectID   *uuid.UUID
	Landmarks  ArtifactRef
	ManualMode bool
	Mask       ProtectionMask
}

// ProposalFaceMasks derives owned mask dependencies from the complete validated
// source-bound report. It does not establish producer authority, verify pixels,
// resolve legacy face identity or grant processing admission. Shared pixels may
// legitimately have different face bindings; contradictory digests may not.
func ProposalFaceMasks(raw []byte, proposal ArtifactRef, source DefectSource) ([]ProposalFaceMask, error) {
	if _, err := ParseProposalProtection(raw, proposal, source); err != nil {
		return nil, err
	}
	var report proposalReportDocument
	if err := decodeExact(raw, &report); err != nil {
		return nil, err
	}
	result := make([]ProposalFaceMask, 0, 2*len(report.Params.Faces))
	seen := map[uuid.UUID]string{}
	for ordinal, face := range report.Params.Faces {
		for _, mask := range []*ProtectionMask{face.Skin, face.Manual} {
			if mask == nil {
				continue
			}
			if digest, exists := seen[mask.Artifact.ID]; exists && digest != mask.Artifact.SHA256 {
				return nil, errors.New("conflicting face mask artifact digests")
			}
			seen[mask.Artifact.ID] = mask.Artifact.SHA256
			// decodeExact allocates private document storage. Copy pointers again so
			// separate returned bindings cannot mutate one another's optional fields.
			owned := *mask
			owned.TransformChain = append([]byte(nil), mask.TransformChain...)
			if mask.ObjectID != nil {
				id := *mask.ObjectID
				owned.ObjectID = &id
			}
			if mask.ModelLockID != nil {
				lock := *mask.ModelLockID
				owned.ModelLockID = &lock
			}
			binding := ProposalFaceMask{Ordinal: ordinal, Landmarks: face.Landmarks, ManualMode: face.ManualMode, Mask: owned}
			if face.ObjectID != nil {
				id := *face.ObjectID
				binding.ObjectID = &id
			}
			result = append(result, binding)
		}
	}
	return result, nil
}
