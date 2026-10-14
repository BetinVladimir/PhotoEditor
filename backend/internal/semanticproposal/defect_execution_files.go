package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Decodes privately retained catalog locations for later transaction locking.
// This preflight grants no authority to queue or to lock arbitrary artifacts.
func (s SQLTransferSourceReader) decodeDefectExecutionFilePins(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectSourceExecutionPreparation) ([]v2draft.InputEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := prepared.source
	if p.tenant != tenant || p.project != s.ProjectID || p.actor != who.UserID || p.session != who.SessionID || p.bucket != s.Bucket || p.binding != binding || !p.unitRGB {
		return nil, v2input.ErrInvalid
	}
	var expected v2qualification.Expected
	if len(prepared.expected) == 0 || len(prepared.expected) > 4<<20 || json.Unmarshal([]byte(prepared.expected), &expected) != nil {
		return nil, v2input.ErrInvalid
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil || string(expectedBody) != prepared.expected {
		return nil, v2input.ErrInvalid
	}
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, prepared.qualification, expected); err != nil {
		return nil, err
	}
	var descriptors struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}
	if len(prepared.descriptors) == 0 || len(prepared.descriptors) > 4<<20 || json.Unmarshal([]byte(prepared.descriptors), &descriptors) != nil {
		return nil, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(descriptors)
	if err != nil || string(body) != prepared.descriptors {
		return nil, v2input.ErrInvalid
	}
	selected, err := validateDefectProfileDescriptors(descriptors.Published, descriptors.Nodes)
	if err != nil {
		return nil, err
	}
	var files []v2draft.InputEvidence
	if len(prepared.files) == 0 || len(prepared.files) > 4<<20 || json.Unmarshal([]byte(prepared.files), &files) != nil || len(files) != len(selected) {
		return nil, v2input.ErrInvalid
	}
	body, err = canonical.Marshal(files)
	if err != nil || string(body) != prepared.files {
		return nil, v2input.ErrInvalid
	}
	byID := make(map[uuid.UUID]v2draft.InputEvidence, len(files))
	for _, entry := range files {
		node, exists := selected[entry.Ref.ArtifactID]
		if !exists || node.Ref != entry.Ref || node.Contract != entry.Contract || entry.Object.SHA256 != entry.Ref.SHA256 || entry.Object.Bytes <= 0 || entry.Object.Bytes > 9007199254740991 || entry.Object.Key == "" || entry.Object.VersionID == "" || entry.Object.VersionID == "null" {
			return nil, v2input.ErrInvalid
		}
		if _, exists := byID[entry.Ref.ArtifactID]; exists {
			return nil, v2input.ErrInvalid
		}
		byID[entry.Ref.ArtifactID] = entry
	}
	sourcePins, err := decodeSourcePreparationPins(p)
	if err != nil {
		return nil, err
	}
	for _, pin := range sourcePins {
		want, err := canonical.Marshal(pin.Node)
		if err != nil {
			return nil, v2input.ErrInvalid
		}
		got, err := canonical.Marshal(selected[pin.Node.Ref.ArtifactID])
		if err != nil || string(want) != string(got) {
			return nil, v2input.ErrConflict
		}
		file, exists := byID[pin.Node.Ref.ArtifactID]
		if !exists || file.Ref != pin.Node.Ref || file.Contract != pin.Node.Contract || file.Object != pin.Object {
			return nil, v2input.ErrConflict
		}
	}
	return files, nil
}
