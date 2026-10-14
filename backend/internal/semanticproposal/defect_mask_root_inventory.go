package semanticproposal

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Matches every original ordered face mask binding to its retained producer root.
// Physical root content, producer qualification and SQL discovery remain separate.
func validateDefectMaskRootInventory(contentBody, parserBody json.RawMessage) error {
	var content defectActivationContent
	var parser defectParserAuthority
	if json.Unmarshal(contentBody, &content) != nil || json.Unmarshal(parserBody, &parser) != nil || parser.SchemaVersion != 1 {
		return v2input.ErrInvalid
	}
	var params struct {
		Faces []review.DefectProposalFaceInput `json:"faces"`
	}
	var roots []review.ProposalFaceMaskRoot
	if json.Unmarshal([]byte(content.ParamsText), &params) != nil || params.Faces == nil || len(params.Faces) > 64 || json.Unmarshal(parser.Roots, &roots) != nil || roots == nil || len(roots) > 128 {
		return v2input.ErrInvalid
	}
	required := make([]review.ProposalFaceMask, 0, 2*len(params.Faces))
	for ordinal, face := range params.Faces {
		for _, mask := range []*review.ProtectionMask{face.Skin, face.Manual} {
			if mask == nil {
				continue
			}
			required = append(required, review.ProposalFaceMask{Ordinal: ordinal, ObjectID: face.ObjectID, Landmarks: face.Landmarks, ManualMode: face.ManualMode, Mask: *mask})
		}
	}
	if len(required) != len(roots) {
		return v2input.ErrInvalid
	}
	digests := map[uuid.UUID]string{}
	for index, root := range roots {
		if root.Root.ID == uuid.Nil {
			return v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(root.Root.SHA256); err != nil {
			return v2input.ErrInvalid
		}
		if sha, exists := digests[root.Root.ID]; exists && sha != root.Root.SHA256 {
			return v2input.ErrInvalid
		}
		digests[root.Root.ID] = root.Root.SHA256
		wanted, err := canonical.Marshal(required[index])
		if err != nil {
			return v2input.ErrInvalid
		}
		actual, err := canonical.Marshal(root.Binding)
		if err != nil || string(wanted) != string(actual) {
			return v2input.ErrInvalid
		}
	}
	return nil
}
