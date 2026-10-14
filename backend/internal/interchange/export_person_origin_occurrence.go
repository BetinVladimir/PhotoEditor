package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ExportPersonOriginOccurrence proves the selected recipe's exact person-mask
// binding through immutable graph edges and repeats physical reads before
// an issuer can publish a receipt.
type ExportPersonOriginOccurrence struct {
	planBody []byte
	roots    []v2input.Entry
	reader   ExportClosureNodeResolver
}

func NewExportPersonOriginOccurrence(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, resolver ExportClosureNodeResolver) (*ExportPersonOriginOccurrence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver == nil {
		return nil, v2input.ErrInvalid
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body())
	if err != nil {
		return nil, err
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil || plan.Format != "photoeditor_bundle" || plan.Direction != "export" {
		return nil, v2input.ErrInvalid
	}
	if factory, ok := resolver.(ExportGraphResolverFactory); ok {
		resolver, err = factory.BindExportGraph(ctx, compiled)
		if err != nil || resolver == nil {
			return nil, v2input.ErrInvalid
		}
	}
	return &ExportPersonOriginOccurrence{planBody: append([]byte(nil), compiled.planBody...), roots: append([]v2input.Entry(nil), compiled.roots...), reader: resolver}, ctx.Err()
}

func readPersonOriginNode(ctx context.Context, resolver ExportClosureNodeResolver, entry v2input.Entry) (ExportClosureNode, error) {
	var zero ExportClosureNode
	node, err := resolver.ResolveExportNode(ctx, entry)
	if err != nil {
		return zero, err
	}
	if node.Entry != entry || rawSHA(node.Body) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(node.Body)
	if err != nil || !bytes.Equal(normal, node.Body) {
		return zero, v2input.ErrInvalid
	}
	return ExportClosureNode{Entry: entry, Body: append([]byte(nil), node.Body...), Children: append([]v2input.Entry(nil), node.Children...)}, ctx.Err()
}

func personOriginChild(node ExportClosureNode, child v2input.Entry) bool {
	found := false
	for _, candidate := range node.Children {
		if candidate.Ref.ArtifactID == child.Ref.ArtifactID {
			if candidate != child || found {
				return false
			}
			found = true
		}
	}
	return found
}

func samePersonOriginNode(left, right ExportClosureNode) bool {
	return left.Entry == right.Entry && bytes.Equal(left.Body, right.Body) && reflect.DeepEqual(left.Children, right.Children)
}

func (a *ExportPersonOriginOccurrence) QualifyPortablePersonOriginOccurrence(ctx context.Context, scope PortablePersonOriginExpectation, rootBody []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || a.reader == nil || scope.EntryID == uuid.Nil || !validImportRef(scope.Recipe) || !validImportRef(scope.Override) || !validImportRef(scope.MaskRef) || !validImportRef(scope.MaskPixels) || len(rootBody) < 2 || len(rootBody) > 1<<20 || rawSHA(rootBody) != scope.MaskRef.SHA256 {
		return v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(rootBody)
	if err != nil || !bytes.Equal(normal, rootBody) {
		return v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(rootBody)
	if err != nil || mask.Selector != "person" || mask.Protected || mask.ObjectID != nil || mask.Artifact.ID != scope.MaskPixels.ArtifactID || mask.Artifact.SHA256 != scope.MaskPixels.SHA256 {
		return v2input.ErrInvalid
	}
	var plan importPlanDocument
	if err := decodeImportDocument(a.planBody, &plan); err != nil || plan.Direction != "export" || plan.Format != "photoeditor_bundle" {
		return v2input.ErrInvalid
	}
	selected := 0
	for _, entry := range plan.Entries {
		if entry.ID == scope.EntryID {
			if entry.Binding == nil || *entry.Binding != scope.Source || entry.Recipe == nil || *entry.Recipe != scope.Recipe {
				return v2input.ErrInvalid
			}
			selected++
		}
	}
	if selected != 1 {
		return v2input.ErrInvalid
	}
	sourceEntry := v2input.Entry{Ref: scope.Source.Source, Contract: "WorkingImage"}
	sourceNode, err := readPersonOriginNode(ctx, a.reader, sourceEntry)
	if err != nil {
		return err
	}
	image, err := review.ParseWorkingImage(sourceNode.Body)
	if err != nil {
		return err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != scope.Source.CanvasHash || mask.Canvas != image.Canvas {
		return v2input.ErrInvalid
	}
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return err
	}
	sourceChain, err := canonical.Bytes(image.TransformChain)
	if err != nil || !bytes.Equal(maskChain, sourceChain) {
		return v2input.ErrInvalid
	}
	overrideEntry := v2input.Entry{Ref: scope.Override, Contract: "Override"}
	var frozenNode ExportClosureNode
	found := 0
	for _, candidate := range a.roots {
		if candidate.Contract == "FrozenRenderRecipe" {
			node, err := readPersonOriginNode(ctx, a.reader, candidate)
			if err != nil {
				return err
			}
			frozen, err := v2draft.DecodeFrozenRenderRecipe(node.Body)
			if err != nil {
				return err
			}
			if frozen.Scalars.Source != scope.Source || frozen.RecipeSHA256 != scope.Recipe.SHA256 {
				continue
			}
			if rawSHA(frozen.Recipe) != scope.Recipe.SHA256 || !personOriginChild(node, overrideEntry) {
				return v2input.ErrInvalid
			}
			var recipe struct {
				Overrides []v2input.Ref `json:"overrides"`
			}
			if json.Unmarshal(frozen.Recipe, &recipe) != nil {
				return v2input.ErrInvalid
			}
			matches := 0
			for _, ref := range recipe.Overrides {
				if ref == scope.Override {
					matches++
				}
			}
			if matches != 1 || found != 0 {
				return v2input.ErrInvalid
			}
			frozenNode = node
			found++
		}
	}
	if found != 1 {
		return v2input.ErrInvalid
	}
	overrideNode, err := readPersonOriginNode(ctx, a.reader, overrideEntry)
	if err != nil {
		return err
	}
	if err := editor.ValidateOverride(overrideNode.Body, image.Canvas.SourceSHA256); err != nil {
		return err
	}
	if !personOriginChild(overrideNode, v2input.Entry{Ref: scope.MaskPixels, Contract: "MaskPixels"}) {
		return v2input.ErrInvalid
	}
	var override struct {
		MaskRefs []struct {
			Purpose   string          `json:"purpose"`
			Confirmed bool            `json:"confirmed"`
			Mask      json.RawMessage `json:"mask"`
		} `json:"mask_refs"`
	}
	if json.Unmarshal(overrideNode.Body, &override) != nil {
		return v2input.ErrInvalid
	}
	inline := 0
	for _, binding := range override.MaskRefs {
		if binding.Purpose == "person_subject" && binding.Confirmed && bytes.Equal(binding.Mask, rootBody) {
			inline++
		}
	}
	if inline != 1 {
		return v2input.ErrInvalid
	}
	for _, expected := range []ExportClosureNode{sourceNode, frozenNode, overrideNode} {
		current, err := readPersonOriginNode(ctx, a.reader, expected.Entry)
		if err != nil || !samePersonOriginNode(current, expected) {
			return v2input.ErrInvalid
		}
	}
	return ctx.Err()
}

var _ PortablePersonOriginOccurrence = (*ExportPersonOriginOccurrence)(nil)
