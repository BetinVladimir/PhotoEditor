package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2observation"
	"strconv"
)

func descriptorShardRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	fields, ok := exactFields(raw, "schema_version", "snapshot_digest", "frames")
	invalid := func() ([]LayerRef, error) { return nil, v2input.ErrInvalid }
	if !ok || !isDocumentRef(parent) {
		return invalid()
	}
	var version int
	var digest string
	var frames []json.RawMessage
	if json.Unmarshal(fields["schema_version"], &version) != nil || version != 1 || json.Unmarshal(fields["snapshot_digest"], &digest) != nil || !correctionDigest(digest) || json.Unmarshal(fields["frames"], &frames) != nil || len(frames) < 1 || len(frames) > 128 {
		return invalid()
	}
	children := []LayerRef{}
	seen := map[int]bool{}
	for i, rawFrame := range frames {
		var head struct {
			Binding v2input.SessionVersionBinding `json:"binding"`
			Ordinal int                           `json:"ordinal"`
		}
		if json.Unmarshal(rawFrame, &head) != nil || head.Binding.AssetVersionID == uuid.Nil || !isDocumentRef(head.Binding.Source) || !correctionDigest(head.Binding.CanvasHash) || seen[head.Ordinal] {
			return invalid()
		}
		frame, err := DecodeFrameFeatures(rawFrame, head.Binding)
		if err != nil {
			return invalid()
		}
		seen[head.Ordinal] = true
		prefix := path + ".frames[" + strconv.Itoa(i) + "]"
		children = append(children, LayerRef{Path: prefix + ".binding.source", Contract: "WorkingImage", Ref: frame.Binding.Source, CanvasHash: frame.Binding.CanvasHash, ParentRef: parent, ExpectedBinding: &head.Binding}, LayerRef{Path: prefix + ".faces", Contract: "FaceObservations", Ref: frame.Faces, ParentRef: parent, ExpectedBinding: &head.Binding})
	}
	return children, nil
}

func faceObservationRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	var doc struct {
		Binding v2input.SessionVersionBinding `json:"binding"`
	}
	if json.Unmarshal(raw, &doc) != nil || !isDocumentRef(parent) {
		return nil, v2input.ErrInvalid
	}
	binding := v2observation.Binding{AssetVersionID: doc.Binding.AssetVersionID, Source: v2observation.ArtifactRef{ArtifactID: doc.Binding.Source.ArtifactID, SHA256: doc.Binding.Source.SHA256}, CanvasHash: doc.Binding.CanvasHash}
	if v2observation.ValidatePreEditFaces(raw, binding) != nil {
		return nil, v2input.ErrInvalid
	}
	return []LayerRef{{Path: path + ".binding.source", Contract: "WorkingImage", Ref: doc.Binding.Source, CanvasHash: doc.Binding.CanvasHash, ParentRef: parent, ExpectedBinding: &doc.Binding}}, nil
}

func validateFaceOccurrence(raw []byte, node LayerRef) error {
	if node.ExpectedBinding == nil {
		return nil
	}
	var doc struct {
		Binding v2input.SessionVersionBinding `json:"binding"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Binding != *node.ExpectedBinding {
		return v2input.ErrInvalid
	}
	return nil
}

// Called after complete descriptorShardRefs validation. The selected frame must
// match the recipe's frozen ordinal and exact source, not merely occur somewhere.
func validateRenderDescriptorOccurrence(raw []byte, node LayerRef) error {
	if node.ExpectedLearning == nil {
		return nil
	}
	learning := node.ExpectedLearning
	if node.ExpectedBinding == nil || node.Ref != learning.Descriptor || learning.VersionID == uuid.Nil || learning.AnalysisID == uuid.Nil || learning.Ordinal < 0 || learning.Ordinal > 4999 {
		return v2input.ErrInvalid
	}
	var shard struct {
		Frames []struct {
			Ordinal int                           `json:"ordinal"`
			Binding v2input.SessionVersionBinding `json:"binding"`
		} `json:"frames"`
	}
	if json.Unmarshal(raw, &shard) != nil {
		return v2input.ErrInvalid
	}
	matches := 0
	for _, frame := range shard.Frames {
		if frame.Ordinal == learning.Ordinal {
			if frame.Binding != *node.ExpectedBinding {
				return v2input.ErrInvalid
			}
			matches++
		}
	}
	if matches != 1 {
		return v2input.ErrInvalid
	}
	return nil
}

func bindRenderDescriptorFrames(raw []byte, node LayerRef, children []LayerRef) error {
	if node.ExpectedLearning == nil {
		return nil
	}
	if node.ExpectedBinding == nil || !isDocumentRef(node.ParentRef) || !correctionDigest(node.ExpectedModelLockSHA) {
		return v2input.ErrInvalid
	}
	var shard struct {
		Frames []struct {
			Ordinal int `json:"ordinal"`
		} `json:"frames"`
	}
	if json.Unmarshal(raw, &shard) != nil || len(children) != len(shard.Frames)*2 {
		return v2input.ErrInvalid
	}
	for i, frame := range shard.Frames {
		for j := 0; j < 2; j++ {
			authority := RenderFrameAuthority{Root: node.ParentRef, Descriptor: node.Ref, SelectedVersion: node.ExpectedBinding.AssetVersionID, Learning: *node.ExpectedLearning, ModelLockSHA: node.ExpectedModelLockSHA, Ordinal: frame.Ordinal}
			children[2*i+j].RenderFrame = &authority
		}
	}
	return nil
}
