package v2draft

import (
	"bytes"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// DustReview is inline in Params; its proposal is one typed dependency.
// Current review authorization is checked by the operation qualifier.
func dustRepairParamRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, bool, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false, v2input.ErrInvalid
	}
	_, image := fields["image"]
	_, proposal := fields["proposal"]
	_, decision := fields["review"]
	if !image || !proposal || !decision {
		return nil, false, nil
	}
	invalid := func() ([]LayerRef, bool, error) { return nil, true, v2input.ErrInvalid }
	if len(fields) != 3 || len(raw) < 2 || len(raw) > 1<<20 {
		return invalid()
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return invalid()
	}
	var p struct {
		Image    v2input.Ref           `json:"image"`
		Proposal *v2input.Ref          `json:"proposal"`
		Review   *review.DustSelection `json:"review"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || !isDocumentRef(p.Image) || (p.Proposal == nil) != (p.Review == nil) {
		return invalid()
	}
	encoded, err := canonical.Marshal(p)
	if err != nil || !bytes.Equal(encoded, raw) {
		return invalid()
	}
	children := []LayerRef{{Path: path + ".image", Contract: "WorkingImage", Ref: p.Image, ParentRef: parent}}
	if p.Proposal != nil {
		if !isDocumentRef(*p.Proposal) || review.ValidateDustSelectionStructure(*p.Review) != nil || p.Review.Proposal.ID != p.Proposal.ArtifactID || p.Review.Proposal.SHA256 != p.Proposal.SHA256 || p.Review.Base.ID != p.Image.ArtifactID || p.Review.Base.SHA256 != p.Image.SHA256 {
			return invalid()
		}
		children = append(children, LayerRef{Path: path + ".proposal", Contract: "DustCandidates", Ref: *p.Proposal, ParentRef: parent})
	}
	return children, true, nil
}
