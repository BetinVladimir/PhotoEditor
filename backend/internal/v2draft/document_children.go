package v2draft

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sort"
	"strconv"
	"strings"
)

// DocumentChildren validates a supported document before exposing references.
// Generic Params still require their operation-specific schema/binding check.
// It does not authorize references or validate their bytes.
// Result children follow schema property order, as required by adoption's
// first-visit inventory. Array order is retained by nestedRefs.
func DocumentChildren(raw []byte, contract string, parent v2input.Ref) ([]LayerRef, error) {
	if !isDocumentRef(parent) {
		return nil, v2input.ErrInvalid
	}
	const path = "Document"
	var children []LayerRef
	var err error
	switch contract {
	case "LayerStack":
		var header struct {
			Source v2input.SessionVersionBinding `json:"source"`
		}
		if json.Unmarshal(raw, &header) != nil {
			return nil, v2input.ErrInvalid
		}
		if !isDocumentRef(v2input.Ref{ArtifactID: header.Source.AssetVersionID, SHA256: header.Source.CanvasHash}) {
			return nil, v2input.ErrInvalid
		}
		children, err = ValidateLayerStack(raw, header.Source)
		for i := range children {
			children[i].ParentRef = parent
			children[i].Path = path + strings.TrimPrefix(children[i].Path, "LayerStack")
		}
	case "Params":
		var handled bool
		children, handled, err = operationParamRefs(raw, path, parent)
		if err == nil && !handled {
			err = nestedRefs(raw, path, contract, parent, 0, &children)
		}
	case "BodyFit":
		children, err = bodyFitDocumentRefs(raw, path, parent)
	case "DefectCandidates":
		if _, err = review.ParseDefectProposalStructure(context.Background(), raw,
			review.ArtifactRef{ID: parent.ArtifactID, SHA256: parent.SHA256}); err == nil {
			err = nestedRefs(raw, path, contract, parent, 0, &children)
			for i := range children {
				switch children[i].Path {
				case path + ".image":
					children[i].Contract = "WorkingImage"
				case path + ".source_pixels":
					children[i].Contract = "WorkingPixels"
				case path + ".label_map":
					children[i].Contract = "DefectLabelMap"
				default:
					if children[i].Contract != "DenseWarpField" {
						err = v2input.ErrInvalid
					}
				}
			}
		}
	case "DustCandidates":
		var header struct {
			Frames []review.ArtifactRef `json:"frames"`
		}
		if json.Unmarshal(raw, &header) != nil || len(header.Frames) < 5 || len(header.Frames) > 64 {
			err = v2input.ErrInvalid
		} else {
			_, err = review.ReadDustProposal(context.Background(), strings.NewReader(string(raw)), review.ArtifactRef{ID: parent.ArtifactID, SHA256: parent.SHA256}, header.Frames[0])
			if err == nil {
				for i, frame := range header.Frames {
					children = append(children, LayerRef{Path: path + ".frames[" + strconv.Itoa(i) + "]", Contract: "WorkingImage", Ref: v2input.Ref{ArtifactID: frame.ID, SHA256: frame.SHA256}, ParentRef: parent})
				}
			}
		}
	case "Stats20":
		var stats review.Stats20
		stats, err = review.ParseStats20(raw)
		if err == nil {
			if stats.SourceImage == nil {
				err = v2input.ErrInvalid
			} else {
				children = append(children, LayerRef{Path: path + ".source_image", Contract: "WorkingImage", ParentRef: parent,
					Ref: v2input.Ref{ArtifactID: stats.SourceImage.ID, SHA256: stats.SourceImage.SHA256}})
			}
		}
	case "DefectReview":
		var selection review.DefectSelection
		selection, err = review.ParseDefectSelection(raw, review.ArtifactRef{ID: parent.ArtifactID, SHA256: parent.SHA256})
		if err == nil {
			for _, child := range []struct {
				path, contract string
				ref            review.ArtifactRef
			}{
				{"image", "WorkingImage", selection.Image},
				{"label_map", "DefectLabelMap", selection.LabelMap},
				{"proposal", "DefectCandidates", selection.Proposal},
			} {
				children = append(children, LayerRef{Path: path + "." + child.path, Contract: child.contract,
					Ref: v2input.Ref{ArtifactID: child.ref.ID, SHA256: child.ref.SHA256}, ParentRef: parent})
			}
		}
	case "FaceObservations":
		children, err = faceObservationRefs(raw, path, parent)
	case "DescriptorShard":
		children, err = descriptorShardRefs(raw, path, parent)
	case "CorrectionPlan":
		var header struct {
			Items []struct {
				Binding v2input.SessionVersionBinding `json:"binding"`
			} `json:"items"`
		}
		if json.Unmarshal(raw, &header) != nil || len(header.Items) == 0 {
			return nil, v2input.ErrInvalid
		}
		// Validate the document independently; the scoped resolver separately
		// requires membership of the task's selected source binding.
		var items []v2input.SessionVersionBinding
		items, err = correctionBindings(raw, header.Items[0].Binding)
		if err == nil {
			for i, item := range items {
				children = append(children, LayerRef{Path: path + ".items[" + strconv.Itoa(i) + "].binding.source", Contract: "WorkingImage", Ref: item.Source, CanvasHash: item.CanvasHash, ParentRef: parent})
			}
		}
	case "CandidateProfile":
		_, err = DecodeCandidateProfile(raw)
	case "FrozenRenderRecipe":
		children, err = renderRecipeRefs(raw, path, parent)
	case "ImageResult":
		children, err = imageResultRefs(raw, path, parent)
		// ImageResult has no array-valued child paths.
		sort.Slice(children, func(i, j int) bool { return children[i].Path < children[j].Path })
	case "ImageReport":
		children, err = imageReportRefs(raw, path, parent)
		// Preserve array order within each property.
		sort.SliceStable(children, func(i, j int) bool {
			return reportProperty(children[i].Path) < reportProperty(children[j].Path)
		})
	case "IdentityReport":
		children, err = identityReportRefs(raw, path, parent)
	case "WorkingImage":
		if _, err = review.ParseWorkingImage(raw); err == nil {
			err = nestedRefs(raw, path, contract, parent, 0, &children)
		}
	case "ManualMask":
		_, err = review.ParseManualVectorsCanvas(raw)
	case "MaskRef":
		if _, err = review.ParseMaskRef(raw); err == nil {
			err = nestedRefs(raw, path, contract, parent, 0, &children)
		}
	default:
		return nil, v2input.ErrInvalid
	}
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	return children, nil
}

func reportProperty(path string) string {
	for i := len("Document."); i < len(path); i++ {
		if path[i] == '[' || path[i] == '.' {
			return path[len("Document."):i]
		}
	}
	return path[len("Document."):]
}

// OpaqueDocumentChildren discovers references using the resolver's bounded
// generic JSON traversal. It does not validate an operation/document schema,
// classify referenced object media or authorize any occurrence.
func OpaqueDocumentChildren(raw []byte, contract string, parent v2input.Ref) ([]LayerRef, error) {
	if !isDocumentRef(parent) || contract == "" || !json.Valid(raw) {
		return nil, v2input.ErrInvalid
	}
	children := []LayerRef{}
	if err := nestedRefs(raw, "Document", contract, parent, 0, &children); err != nil {
		return nil, err
	}
	return children, nil
}

// DocumentOccurrenceChildren derives child context from verified parent bytes.
// It provides structural provenance only; catalog scope and bytes remain separate.
func DocumentOccurrenceChildren(raw []byte, node LayerRef) ([]LayerRef, error) {
	var children []LayerRef
	var err error
	if node.Contract == "RasterizedMask" {
		// A root alone cannot distinguish inline Params from external ManualMask.
		// This context must come from verified original producer documents.
		children, err = manualRasterRootRefs(raw, "Document", node.Ref, node.ManualVectors)
	} else {
		children, err = DocumentChildren(raw, node.Contract, node.Ref)
	}
	if err != nil {
		return nil, err
	}
	switch node.Contract {
	case "WorkingImage":
		var image review.WorkingImage
		image, err = review.ParseWorkingImage(raw)
		if err == nil && node.CanvasHash != "" {
			var hash string
			hash, err = canonical.Hash(image.Canvas)
			if err == nil && hash != node.CanvasHash {
				err = v2input.ErrInvalid
			}
		}
	case "ManualMask":
		var canvas review.Canvas
		canvas, err = review.ParseManualVectorsCanvas(raw)
		if err == nil && node.CanvasHash != "" {
			var hash string
			hash, err = canonical.Hash(canvas)
			if err == nil && hash != node.CanvasHash {
				err = v2input.ErrInvalid
			}
		}
	case "DescriptorShard":
		if err = validateRenderDescriptorOccurrence(raw, node); err == nil {
			err = bindRenderDescriptorFrames(raw, node, children)
		}
	case "FaceObservations":
		err = validateFaceOccurrence(raw, node)
	}
	if err != nil {
		return nil, err
	}
	for i := range children {
		children[i].Path = node.Path + strings.TrimPrefix(children[i].Path, "Document")
		if children[i].ExpectedBinding == nil && node.ExpectedBinding != nil {
			binding := *node.ExpectedBinding
			children[i].ExpectedBinding = &binding
		}
		if children[i].RenderFrame == nil && node.RenderFrame != nil {
			frame := *node.RenderFrame
			children[i].RenderFrame = &frame
		}
	}
	return children, nil
}
