package v2render

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"strings"
)

func cloneRenderOccurrences(records []v2draft.OccurrenceWitness) []v2draft.OccurrenceWitness {
	if records == nil {
		return nil
	}
	owned := append([]v2draft.OccurrenceWitness(nil), records...)
	for i := range owned {
		node := &owned[i].Node
		if node.ExpectedBinding != nil {
			v := *node.ExpectedBinding
			node.ExpectedBinding = &v
		}
		if node.ExpectedLearning != nil {
			v := *node.ExpectedLearning
			node.ExpectedLearning = &v
		}
		if node.RenderFrame != nil {
			v := *node.RenderFrame
			node.RenderFrame = &v
		}
	}
	return owned
}

// Occurrence metadata is scoped evidence, not binary-byte verification or ACL.
func validateRenderOccurrences(request RenderDraftRequest, entries []v2input.Entry) error {
	records := request.ClosureOccurrences
	if len(records) == 0 {
		if len(request.LayerDocuments) > 0 || hasEditorExecutedReceipt(request.RecipeRaw) {
			return v2input.ErrInvalid
		}
		return nil
	}
	if len(records) > 4096 {
		return v2input.ErrInvalid
	}
	total := 0
	seen := map[v2input.Ref]bool{}
	objects := map[v2input.Ref]objectstore.Object{}
	for _, record := range records {
		node, obj := record.Node, record.Object
		if !validLayerArtifactRef(node.Ref) || node.Path == "" || len(node.Contract) < 1 || len(node.Contract) > 120 || obj.SHA256 != node.Ref.SHA256 || obj.Bytes < 1 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.ContentType == "" {
			return v2input.ErrInvalid
		}
		if prior, exists := objects[node.Ref]; exists && prior != obj {
			return v2input.ErrInvalid
		}
		objects[node.Ref] = obj
		wire, err := json.Marshal(record)
		if err != nil || len(wire) > (32<<20)-total {
			return v2input.ErrInvalid
		}
		total += len(wire)
		contract := node.Contract
		switch contract {
		case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField":
			if obj.ContentType != "application/x-npy" && obj.ContentType != "application/octet-stream" {
				return v2input.ErrInvalid
			}
			contract = "NpyArray"
		case "Artifact":
			// Generic refs retain catalog media classification.
		default:
			if obj.ContentType != "application/json" {
				return v2input.ErrInvalid
			}
		}
		if entries != nil {
			found := false
			for _, entry := range entries {
				if entry.Ref == node.Ref && (contract == entry.Contract || node.Contract == "Artifact") {
					found = true
					break
				}
			}
			if !found {
				return v2input.ErrInvalid
			}
		}
		if obj.ContentType == "application/json" {
			found := false
			for _, document := range request.ClosureDocuments {
				if document.Ref == node.Ref && document.Object == obj && document.Path == node.Path && document.Contract == node.Contract && document.Parent == node.ParentRef {
					found = true
					break
				}
			}
			if !found {
				return v2input.ErrInvalid
			}
		}
		seen[node.Ref] = true
	}
	for _, entry := range entries {
		if !seen[entry.Ref] {
			return v2input.ErrInvalid
		}
	}
	for _, document := range request.ClosureDocuments {
		found := false
		for _, record := range records {
			node := record.Node
			if node.Ref == document.Ref && node.Path == document.Path && node.Contract == document.Contract && node.ParentRef == document.Parent && record.Object == document.Object {
				found = true
				break
			}
		}
		if !found {
			return v2input.ErrInvalid
		}
	}
	if len(request.LayerDocuments) > 0 || hasEditorExecutedReceipt(request.RecipeRaw) {
		if err := validateRenderOccurrenceEdges(request); err != nil {
			return err
		}
	}
	return nil
}

// Parse each JSON occurrence once and index by full path, avoiding repeated
// descriptor parsing for each frame/descendant. Validate both edge directions.
func validateRenderOccurrenceEdges(request RenderDraftRequest) error {
	nodes := make(map[string]v2draft.LayerRef, len(request.ClosureOccurrences))
	for _, record := range request.ClosureOccurrences {
		n := record.Node
		if _, duplicate := nodes[n.Path]; duplicate {
			return v2input.ErrInvalid
		}
		nodes[n.Path] = n
	}
	authorized := make(map[string]bool, len(nodes))
	edges := make(map[string][]string, len(nodes))
	for _, document := range request.ClosureDocuments {
		parent, exists := nodes[document.Path]
		if !exists || parent.Ref != document.Ref || parent.Contract != document.Contract || parent.ParentRef != document.Parent {
			return v2input.ErrInvalid
		}
		children, err := renderOccurrenceChildren(document, parent)
		if err != nil {
			return err
		}
		for _, child := range children {
			node, exists := nodes[child.Path]
			if !exists || !renderDerivedChildMatches(child, node) {
				return v2input.ErrInvalid
			}
			authorized[node.Path] = true
			edges[parent.Path] = append(edges[parent.Path], node.Path)
		}
	}
	sourcePath, recipePath := "TaskInputs[1]", "TaskInputs[0]"
	if bytes.Compare(request.Binding.Source.ArtifactID[:], request.RecipeRef.ArtifactID[:]) < 0 {
		sourcePath, recipePath = recipePath, sourcePath
	}
	for _, record := range request.ClosureOccurrences {
		node := record.Node
		if node.ParentRef != (v2input.Ref{}) {
			if !authorized[node.Path] {
				return v2input.ErrInvalid
			}
			continue
		}
		if node.ExpectedBinding != nil || node.ExpectedLearning != nil || node.ExpectedModelLockSHA != "" || node.RenderFrame != nil {
			return v2input.ErrInvalid
		}
		if node.Ref == request.RecipeRef && node.Contract == "FrozenRenderRecipe" && node.Path == recipePath {
			continue
		}
		if node.Ref == request.Binding.Source && node.Contract == "WorkingImage" && node.Path == sourcePath && node.CanvasHash == request.Binding.CanvasHash {
			continue
		}
		return v2input.ErrInvalid
	}
	return validateRenderOccurrenceReachability(nodes, edges, []string{sourcePath, recipePath})
}

func validateRenderOccurrenceReachability(nodes map[string]v2draft.LayerRef, edges map[string][]string, roots []string) error {
	depths := make(map[string]int, len(nodes))
	queue := make([]string, 0, len(nodes))
	for _, root := range roots {
		if _, exists := nodes[root]; !exists {
			return v2input.ErrInvalid
		}
		if _, visited := depths[root]; visited {
			continue
		}
		depths[root] = 0
		queue = append(queue, root)
	}
	for cursor := 0; cursor < len(queue); cursor++ {
		parent := queue[cursor]
		for _, child := range edges[parent] {
			if _, exists := nodes[child]; !exists || depths[parent] >= 32 {
				return v2input.ErrInvalid
			}
			if _, visited := depths[child]; visited {
				continue
			}
			depths[child] = depths[parent] + 1
			queue = append(queue, child)
		}
	}
	if len(depths) != len(nodes) {
		return v2input.ErrInvalid
	}
	return nil
}

func renderDerivedChildMatches(child, node v2draft.LayerRef) bool {
	return child.Ref == node.Ref && child.ParentRef == node.ParentRef && child.Path == node.Path && (child.Contract == node.Contract || child.Contract == "Artifact") && (child.CanvasHash == "" || child.CanvasHash == node.CanvasHash) && reflect.DeepEqual(child.ExpectedBinding, node.ExpectedBinding) && reflect.DeepEqual(child.ExpectedLearning, node.ExpectedLearning) && child.ExpectedModelLockSHA == node.ExpectedModelLockSHA && reflect.DeepEqual(child.RenderFrame, node.RenderFrame)
}

func renderOccurrenceChildren(parent v2draft.DocumentWitness, node v2draft.LayerRef) ([]v2draft.LayerRef, error) {
	children, err := v2draft.DocumentOccurrenceChildren([]byte(parent.JCS), node)
	if err == nil {
		return children, nil
	}
	switch node.Contract {
	case "EditorExecutedBaseReceipt", "LayerStack", "Params", "FrozenRenderRecipe", "WorkingImage", "MaskRef", "ImageResult", "ImageReport", "IdentityReport", "FaceObservations", "DescriptorShard", "CandidateProfile", "CorrectionPlan":
		return nil, err
	}
	children, err = v2draft.OpaqueDocumentChildren([]byte(parent.JCS), node.Contract, node.Ref)
	if err != nil {
		return nil, err
	}
	for i := range children {
		children[i].Path = node.Path + strings.TrimPrefix(children[i].Path, "Document")
		children[i].ExpectedBinding = node.ExpectedBinding
		children[i].RenderFrame = node.RenderFrame
	}
	return children, nil
}
