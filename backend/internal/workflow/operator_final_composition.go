package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// OperatorFinalComposition contains unsealed local lower-compute inputs only.
// Evidence is the physical measured Apply journal, deliberately NOT an
// EditorExecutedBaseReceipt; normal SQL admission cannot accept it as one.
type OperatorFinalComposition struct {
	Item           uuid.UUID                     `json:"item_id"`
	Task           uuid.UUID                     `json:"task_id"`
	OriginalRecipe review.ArtifactRef            `json:"original_recipe"`
	Recipe         review.ArtifactRef            `json:"recipe"`
	Evidence       review.ArtifactRef            `json:"evidence"`
	Processed      v2input.SessionVersionBinding `json:"processed_source"`
	ExecutedStyle  review.ArtifactRef            `json:"executed_style"`
}

func retainOperatorFinalDocument(ctx context.Context, run uuid.UUID, purpose string, raw []byte, ledger *OperatorApplyLedger, objects *OperatorPhysicalObjects) (review.ArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return review.ArtifactRef{}, err
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) || len(raw) < 2 || len(raw) > 8<<20 {
		return review.ArtifactRef{}, errors.New("operator final document invalid")
	}
	sum := sha256.Sum256(raw)
	pin := hex.EncodeToString(sum[:])
	id := uuid.NewSHA1(run, []byte("operator-final/"+purpose+"/"+pin))
	if err = operatorObjectBudget(ledger, objects.maxObjectBytes, int64(len(raw))); err != nil {
		return review.ArtifactRef{}, err
	}
	path := objects.root + string(os.PathSeparator) + id.String()
	if err = writeOperatorExclusive(path, raw); err != nil {
		return review.ArtifactRef{}, err
	}
	a := styleArtifact{ArtifactID: id, SHA256: pin, Bytes: int64(len(raw)), Bucket: "operator-measurement", Key: id.String(), VersionID: pin, MediaType: "application/json"}
	ledger.objects[id] = a
	return a.ref(), nil
}

func compileOperatorFinalCompositions(ctx context.Context, spec OperatorCaseSpec, ledger *OperatorApplyLedger, objects *OperatorPhysicalObjects, targets []OperatorCaseTarget, style, evidence json.RawMessage) ([]OperatorFinalComposition, error) {
	fail := func() ([]OperatorFinalComposition, error) {
		return nil, errors.New("operator final original/computed source binding invalid")
	}
	if len(spec.OriginalRecipes) != len(targets) || len(targets) != len(ledger.inputs) {
		return fail()
	}
	measured, err := retainOperatorFinalDocument(ctx, spec.Run, "measured-apply", evidence, ledger, objects)
	if err != nil {
		return nil, err
	}
	executedStyle, err := retainOperatorFinalDocument(ctx, spec.Run, "executed-style", style, ledger, objects)
	if err != nil {
		return nil, err
	}
	refs := func(ref review.ArtifactRef) v2input.Ref { return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256} }
	out := make([]OperatorFinalComposition, 0, len(targets))
	for _, target := range targets {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		originalRef, exists := spec.OriginalRecipes[target.Item]
		if !exists {
			return fail()
		}
		originalObject, exists := ledger.objects[originalRef.ID]
		if !exists || originalObject.SHA256 != originalRef.SHA256 {
			return fail()
		}
		originalBody, e := readApplyJSON(ctx, objects, "operator-measurement", originalObject)
		if e != nil {
			return nil, e
		}
		original, e := v2draft.DecodeFrozenRenderRecipe(originalBody)
		if e != nil {
			return nil, e
		}
		var source *applyInput
		for i := range ledger.inputs {
			if ledger.inputs[i].ItemID == target.Item {
				source = &ledger.inputs[i]
				break
			}
		}
		if source == nil || original.Scalars.Source.AssetVersionID != source.VersionID || original.Scalars.Source.Source != refs(source.Image.ref()) {
			return fail()
		}
		canvas, e := canonical.Hash(source.Working.Canvas)
		if e != nil || original.Scalars.Source.CanvasHash != canvas {
			return fail()
		}
		processedObject, exists := ledger.objects[target.Target.ID]
		if !exists || processedObject.SHA256 != target.Target.SHA256 {
			return fail()
		}
		processed, e := readWorking(ctx, objects, "operator-measurement", processedObject)
		if e != nil {
			return nil, e
		}
		canvas, e = canonical.Hash(processed.Canvas)
		if e != nil {
			return nil, e
		}
		binding := v2input.SessionVersionBinding{AssetVersionID: source.VersionID, Source: refs(target.Target), CanvasHash: canvas}
		body, e := v2draft.CompileUnsealedEditorFinalIdentity(originalBody, refs(originalRef), binding, refs(executedStyle), refs(measured))
		if e != nil {
			return nil, e
		}
		recipe, e := retainOperatorFinalDocument(ctx, spec.Run, "identity/"+target.Item.String(), body, ledger, objects)
		if e != nil {
			return nil, e
		}
		out = append(out, OperatorFinalComposition{Item: target.Item, Task: uuid.NewSHA1(spec.Run, []byte("operator/final/"+target.Item.String())), OriginalRecipe: originalRef, Recipe: recipe, Evidence: measured, Processed: binding, ExecutedStyle: executedStyle})
	}
	return out, nil
}

// Every locally published control document participates in the same cumulative
// retained-object budget as measured worker output; reject before writing.
func operatorObjectBudget(ledger *OperatorApplyLedger, limit, extra int64) error {
	if ledger == nil || limit < 1 || extra < 1 || extra > limit {
		return errors.New("operator retained object budget invalid")
	}
	used := int64(0)
	for _, object := range ledger.objects {
		if object.Bytes < 1 || object.Bytes > limit-used {
			return errors.New("operator retained object budget exceeded")
		}
		used += object.Bytes
	}
	if extra > limit-used {
		return errors.New("operator retained object budget exceeded")
	}
	return nil
}
