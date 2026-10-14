package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
)

type renderWitnessResolver interface {
	ResolveTaskInputsWithWitnesses(context.Context, auth.Identity, uuid.UUID, []v2input.Entry, v2input.SessionVersionBinding) ([]v2input.Entry, []v2draft.DocumentWitness, error)
}

type renderEvidenceResolver interface {
	ResolveTaskInputEvidence(context.Context, auth.Identity, uuid.UUID, []v2input.Entry, v2input.SessionVersionBinding) (v2draft.TaskInputEvidence, error)
}

func resolveRenderClosureDocuments(ctx context.Context, request RenderDraftRequest, resolver CompositionInputResolver) ([]v2input.Entry, []v2draft.DocumentWitness, error) {
	evidence, err := resolveRenderClosureEvidence(ctx, request, resolver)
	return evidence.Entries, evidence.Documents, err
}

// Nonempty authored stacks require complete read and occurrence evidence from
// one scoped traversal. Empty stack identity retains the original inventory path.
func resolveRenderClosureEvidence(ctx context.Context, request RenderDraftRequest, resolver CompositionInputResolver) (v2draft.TaskInputEvidence, error) {
	invalid := func(err error) (v2draft.TaskInputEvidence, error) { return v2draft.TaskInputEvidence{}, err }
	if err := ctx.Err(); err != nil {
		return invalid(err)
	}
	if request.Tenant == uuid.Nil || request.Actor.UserID == uuid.Nil || request.Actor.SessionID == uuid.Nil || resolver == nil {
		return invalid(v2input.ErrInvalid)
	}
	if len(request.LayerDocuments) == 0 {
		entries, err := ResolveRenderInputs(ctx, request.Actor, request.Tenant, request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw, resolver)
		if err != nil {
			return invalid(err)
		}
		return v2draft.TaskInputEvidence{Entries: entries}, nil
	}
	provider, ok := resolver.(renderEvidenceResolver)
	if !ok {
		return invalid(v2input.ErrInvalid)
	}
	roots, err := RenderInputRoots(request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw)
	if err != nil {
		return invalid(err)
	}
	evidence, err := provider.ResolveTaskInputEvidence(ctx, request.Actor, request.Tenant, roots, request.Binding)
	if err != nil {
		return invalid(err)
	}
	evidence.Entries, err = ResolveRenderInputs(ctx, request.Actor, request.Tenant, request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw, renderResolvedInventory{evidence.Entries})
	if err != nil {
		return invalid(err)
	}
	request.ClosureDocuments = evidence.Documents
	request.ClosureOccurrences = evidence.Occurrences
	if err = validateRenderClosureDocuments(evidence.Documents, evidence.Entries); err != nil {
		return invalid(err)
	}
	if err = validateRenderClosureCoverage(request); err != nil {
		return invalid(err)
	}
	if err = validateRenderOccurrences(request, evidence.Entries); err != nil {
		return invalid(err)
	}
	evidence.Documents = append([]v2draft.DocumentWitness(nil), evidence.Documents...)
	evidence.Occurrences = cloneRenderOccurrences(evidence.Occurrences)
	return evidence, nil
}

// Coverage of known typed JSON edges is separate from byte validity. Generic
// Artifact refs can be binary and still require scoped catalog classification.
func validateRenderClosureCoverage(request RenderDraftRequest) error {
	if len(request.ClosureDocuments) == 0 {
		if len(request.LayerDocuments) > 0 {
			return v2input.ErrInvalid
		}
		return nil
	}
	type requiredDocument struct {
		ref      v2input.Ref
		contract string
		body     string
	}
	required := []requiredDocument{{request.RecipeRef, "FrozenRenderRecipe", string(request.RecipeRaw)}, {request.Binding.Source, "WorkingImage", string(request.SourceRaw)}}
	addChildren := func(raw []byte, contract string, parent v2input.Ref) error {
		children, err := v2draft.DocumentChildren(raw, contract, parent)
		if err != nil {
			return err
		}
		for _, child := range children {
			switch child.Contract {
			case "Artifact", "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField":
				continue
			}
			required = append(required, requiredDocument{ref: child.Ref, contract: child.Contract})
		}
		return nil
	}
	if err := addChildren(request.RecipeRaw, "FrozenRenderRecipe", request.RecipeRef); err != nil {
		return err
	}
	for _, layer := range request.LayerDocuments {
		required = append(required, requiredDocument{layer.Ref, layer.Contract, layer.JCS})
		if err := addChildren([]byte(layer.JCS), layer.Contract, layer.Ref); err != nil {
			return err
		}
	}
	for _, document := range request.ClosureDocuments {
		switch document.Contract {
		case "LayerStack", "Params", "FrozenRenderRecipe", "WorkingImage", "MaskRef", "ImageResult", "ImageReport", "IdentityReport", "FaceObservations", "DescriptorShard", "CandidateProfile", "CorrectionPlan":
			if err := addChildren([]byte(document.JCS), document.Contract, document.Ref); err != nil {
				return err
			}
		}
	}
	for _, expected := range required {
		found := false
		for _, document := range request.ClosureDocuments {
			if document.Ref == expected.ref && document.Contract == expected.contract && (expected.body == "" || document.JCS == expected.body) {
				found = true
				break
			}
		}
		if !found {
			return v2input.ErrInvalid
		}
	}
	edges := make([][]int, len(request.ClosureDocuments))
	depths := make([]int, len(request.ClosureDocuments))
	for i := range depths {
		depths[i] = -1
	}
	queue := []int{}
	for index, document := range request.ClosureDocuments {
		if document.Parent == (v2input.Ref{}) {
			if !(document.Ref == request.RecipeRef && document.Contract == "FrozenRenderRecipe" || document.Ref == request.Binding.Source && document.Contract == "WorkingImage") {
				return v2input.ErrInvalid
			}
			depths[index] = 0
			queue = append(queue, index)
			continue
		}
		parentFound := false
		for parentIndex, parent := range request.ClosureDocuments {
			if parent.Ref != document.Parent {
				continue
			}
			children, err := v2draft.DocumentChildren([]byte(parent.JCS), parent.Contract, parent.Ref)
			if err != nil {
				switch parent.Contract {
				case "LayerStack", "Params", "FrozenRenderRecipe", "WorkingImage", "MaskRef", "ImageResult", "ImageReport", "IdentityReport", "FaceObservations", "DescriptorShard", "CandidateProfile", "CorrectionPlan":
					return err
				}
				children, err = v2draft.OpaqueDocumentChildren([]byte(parent.JCS), parent.Contract, parent.Ref)
				if err != nil {
					return err
				}
			}
			for _, child := range children {
				if child.Ref == document.Ref && (child.Contract == document.Contract || child.Contract == "Artifact") && document.Path == parent.Path+strings.TrimPrefix(child.Path, "Document") {
					parentFound = true
					edges[parentIndex] = append(edges[parentIndex], index)
					break
				}
			}
		}
		if !parentFound {
			return v2input.ErrInvalid
		}
	}
	for cursor := 0; cursor < len(queue); cursor++ {
		parent := queue[cursor]
		for _, child := range edges[parent] {
			if depths[child] >= 0 {
				continue
			}
			if depths[parent] >= 32 {
				return v2input.ErrInvalid
			}
			depths[child] = depths[parent] + 1
			queue = append(queue, child)
		}
	}
	for _, depth := range depths {
		if depth < 0 {
			return v2input.ErrInvalid
		}
	}
	return nil
}

// Byte evidence is not a persisted ACL grant or a complete occurrence graph.
func validateRenderClosureDocuments(documents []v2draft.DocumentWitness, entries []v2input.Entry) error {
	if len(documents) > 4096 {
		return v2input.ErrInvalid
	}
	total := 0
	for _, document := range documents {
		if len(document.JCS) < 2 || len(document.JCS) > 8<<20 || len(document.JCS) > (32<<20)-total || document.Path == "" || !validLayerArtifactRef(document.Ref) || document.Contract == "" {
			return v2input.ErrInvalid
		}
		total += len(document.JCS)
		obj := document.Object
		if obj.ContentType != "application/json" || obj.VersionID == "" || obj.VersionID == "null" || obj.Key == "" || obj.SHA256 != document.Ref.SHA256 || obj.Bytes != int64(len(document.JCS)) {
			return v2input.ErrInvalid
		}
		body := []byte(document.JCS)
		sum := sha256.Sum256(body)
		wire, err := canonical.Bytes(body)
		if err != nil || !bytes.Equal(wire, body) || hex.EncodeToString(sum[:]) != document.Ref.SHA256 {
			return v2input.ErrInvalid
		}
		if entries != nil {
			present := false
			for _, entry := range entries {
				if entry.Ref == document.Ref && (entry.Contract == document.Contract || document.Contract == "Artifact") {
					present = true
					break
				}
			}
			if !present {
				return v2input.ErrInvalid
			}
		}
	}
	return nil
}
