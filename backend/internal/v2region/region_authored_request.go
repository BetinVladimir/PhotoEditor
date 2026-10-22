package v2region

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// AuthoredRegionRequest names the registered frozen document and the exact
// saved draft that produced it. No model/runtime/policy or object keys are
// caller-selected. This separate contract does not widen scalar region.preview.
type AuthoredRegionRequest struct {
	Preview        RegionPreviewRequest `json:"preview"`
	DraftDocument  uuid.UUID            `json:"draft_document_id"`
	DraftRevision  int64                `json:"draft_revision"`
	FrozenDocument uuid.UUID            `json:"frozen_document_id"`
}

func DecodeAuthoredRegionRequest(raw []byte) (AuthoredRegionRequest, error) {
	var out AuthoredRegionRequest
	if len(raw) == 0 || len(raw) > 16384 {
		return out, ErrRegionInvalid
	}
	body, err := canonical.Bytes(raw)
	if err != nil {
		return out, ErrRegionInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != 4 {
		return out, ErrRegionInvalid
	}
	for _, key := range []string{"preview", "draft_document_id", "draft_revision", "frozen_document_id"} {
		if len(fields[key]) == 0 || string(fields[key]) == "null" {
			return out, ErrRegionInvalid
		}
	}
	preview, err := DecodeRegionPreviewRequest(fields["preview"])
	if err != nil {
		return out, err
	}
	if json.Unmarshal(body, &out) != nil || out.DraftDocument == uuid.Nil || out.FrozenDocument == uuid.Nil || out.DraftDocument == out.FrozenDocument || out.DraftRevision < 1 || out.DraftRevision > 9007199254740991 {
		return AuthoredRegionRequest{}, ErrRegionInvalid
	}
	out.Preview = preview
	return out, nil
}

// ValidateIntent joins public immutable intent to server-resolved evidence.
// It is a consistency check, never an authorization or physical verification.
func (r AuthoredRegionRequest) ValidateIntent(i AuthoredRegionIntent) error {
	if _, err := i.SHA256(); err != nil {
		return err
	}
	if r.DraftDocument != i.DraftDocument || r.DraftRevision != i.DraftRevision || r.FrozenDocument != i.FrozenDocument || r.Preview.VersionID != i.Binding.AssetVersionID || r.Preview.Recipe.ArtifactID != i.Recipe.ArtifactID || r.Preview.Recipe.SHA256 != i.Recipe.SHA256 || r.Preview.Rect != i.Rect || r.Preview.MaxEdge != i.MaxEdge {
		return ErrRegionInvalid
	}
	return nil
}

func authoredRegionRequestBytes(r AuthoredRegionRequest) ([]byte, error) { return canonical.Marshal(r) }
