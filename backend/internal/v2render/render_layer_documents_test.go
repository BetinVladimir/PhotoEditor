package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func layerDocumentFixture(t *testing.T) (CompiledRenderEvidence, []byte, []RenderLayerDocument) {
	t.Helper()
	ref := func(raw []byte) v2input.Ref {
		s := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(s[:])}
	}
	canvas := review.Canvas{Width: 2, Height: 1, Orientation: 1, SpaceID: "source", SourceSHA256: strings.Repeat("a", 64)}
	canvasSHA, _ := canonical.Hash(canvas)
	chainSHA, _ := canonical.Hash(map[string]any{"steps": []any{}})
	chain, _ := canonical.Marshal(map[string]any{"steps": []any{}, "hash": chainSHA})
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref([]byte("source")), CanvasHash: canvasSHA}
	params, _ := canonical.Marshal(map[string]any{"schema_version": 1, "ev": 0.2})
	mask, _ := canonical.Marshal(review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("b", 64)},
		Canvas: canvas, TransformChain: chain, Selector: "manual", Confidence: 1, Protected: false})
	paramsRef, maskRef := ref(params), ref(mask)
	raw, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{map[string]any{
		"id": uuid.New(), "enabled": false, "opacity": 0, "blend": "normal", "operation": "adjust.exposure", "operation_version": "1",
		"params": paramsRef, "mask": map[string]any{"op": "ref", "mask": maskRef, "canvas_hash": canvasSHA}}}})
	root := ref(raw)
	evidence := CompiledRenderEvidence{Source: review.WorkingImage{Canvas: canvas, TransformChain: chain}}
	evidence.Recipe.Scalars.Source = binding
	evidence.Recipe.Recipe, _ = canonical.Marshal(map[string]any{"layer_stack": root})
	refs, err := renderLayerDocumentRefs(evidence, raw)
	if err != nil {
		t.Fatal(err)
	}
	docs := []RenderLayerDocument{{refs[0].Path, refs[0].Contract, paramsRef, root, string(params)},
		{refs[1].Path, refs[1].Contract, maskRef, root, string(mask)}}
	return evidence, raw, docs
}

func TestLayerDocumentsPreserveDisabledOccurrencesAndPinnedBytes(t *testing.T) {
	evidence, raw, docs := layerDocumentFixture(t)
	if err := ValidateRenderLayerDocuments(evidence, raw, docs); err != nil {
		t.Fatal(err)
	}
	calls := 0
	loaded, err := loadRenderLayerDocuments(evidence, raw, func(ref v2draft.LayerRef) ([]byte, error) {
		expected := docs[calls]
		calls++
		if ref.Ref != expected.Ref || ref.Path != expected.Path || ref.ParentRef != expected.Parent || ref.Contract != expected.Contract {
			t.Fatal("scope occurrence changed")
		}
		return []byte(expected.JCS), nil
	})
	if err != nil || calls != 2 || len(loaded) != 2 || loaded[0] != docs[0] || loaded[1] != docs[1] {
		t.Fatal("proof changed", err)
	}
	wire, _ := canonical.Marshal(loaded)
	var restored []RenderLayerDocument
	if json.Unmarshal(wire, &restored) != nil || ValidateRenderLayerDocuments(evidence, raw, restored) != nil {
		t.Fatal("frozen witness cannot restore")
	}
}

func TestLayerWitnessInventoryRequiresDocumentsAndBinaryChildren(t *testing.T) {
	_, _, documents := layerDocumentFixture(t)
	var mask review.MaskRef
	if json.Unmarshal([]byte(documents[1].JCS), &mask) != nil {
		t.Fatal("mask fixture")
	}
	entries := []v2input.Entry{
		{Ref: documents[0].Ref, Contract: "Params"},
		{Ref: documents[1].Ref, Contract: "MaskRef"},
		{Ref: v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}, Contract: "NpyArray"},
	}
	if err := validateRenderLayerInventory(documents, entries); err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		missing := append([]v2input.Entry(nil), entries[:i]...)
		missing = append(missing, entries[i+1:]...)
		if !errors.Is(validateRenderLayerInventory(documents, missing), v2input.ErrInvalid) {
			t.Fatal("missing input accepted", i)
		}
		changed := append([]v2input.Entry(nil), entries...)
		changed[i].Ref.SHA256 = strings.Repeat("d", 64)
		if !errors.Is(validateRenderLayerInventory(documents, changed), v2input.ErrInvalid) {
			t.Fatal("changed SHA accepted", i)
		}
		changed = append([]v2input.Entry(nil), entries...)
		changed[i].Contract = "ImageResult"
		if !errors.Is(validateRenderLayerInventory(documents, changed), v2input.ErrInvalid) {
			t.Fatal("changed contract accepted", i)
		}
	}
}

func TestLayerDocumentsRejectMissingExtraChangedOrForeignEvidence(t *testing.T) {
	for _, kind := range []string{"missing", "extra", "path", "parent", "ref", "contract", "bytes", "noncanonical", "geometry", "root"} {
		t.Run(kind, func(t *testing.T) {
			evidence, raw, docs := layerDocumentFixture(t)
			switch kind {
			case "missing":
				docs = docs[:1]
			case "extra":
				docs = append(docs, docs[0])
			case "path":
				docs[0].Path = "other.params"
			case "parent":
				docs[0].Parent.ArtifactID = uuid.New()
			case "ref":
				docs[0].Ref.ArtifactID = uuid.New()
			case "contract":
				docs[0].Contract = "Artifact"
			case "bytes":
				docs[0].JCS = `{"ev":1}`
			case "noncanonical":
				docs[0].JCS += " "
			case "geometry":
				evidence.Source.TransformChain = json.RawMessage(`{"steps":[],"hash":"different"}`)
			case "root":
				raw = append(raw, ' ')
			}
			if !errors.Is(ValidateRenderLayerDocuments(evidence, raw, docs), v2input.ErrInvalid) {
				t.Fatal("changed proof accepted")
			}
		})
	}
	evidence, raw, _ := layerDocumentFixture(t)
	if _, err := loadRenderLayerDocuments(evidence, raw, func(v2draft.LayerRef) ([]byte, error) { return nil, context.Canceled }); !errors.Is(err, context.Canceled) {
		t.Fatal("reader cancellation lost")
	}
	if _, err := loadRenderLayerDocuments(evidence, raw, func(v2draft.LayerRef) ([]byte, error) { return []byte(strings.Repeat("x", 1048577)), nil }); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("document budget ignored")
	}
}

func TestLayerDocumentsRejectMalformedDescendantsWithConsistentHashes(t *testing.T) {
	deep := `{"value":0}`
	for i := 0; i < 34; i++ {
		deep = `{"nested":` + deep + `}`
	}
	for _, sample := range []struct {
		body  string
		valid bool
	}{
		{`{"settings":{"input":{"artifact_id":"bad","sha256":"bad"}}}`, false},
		{`{"settings":{"input":{"artifact_id":"` + uuid.New().String() + `","sha256":"` + strings.Repeat("c", 64) + `","extra":null}}}`, false},
		{`{"proposal_result":null}`, false},
		{deep, false},
		{`{"settings":{"input":{"artifact_id":"` + uuid.New().String() + `","sha256":"` + strings.Repeat("c", 64) + `"}}}`, true},
	} {
		body := sample.body
		t.Run(body[:20], func(t *testing.T) {
			evidence, raw, documents := layerDocumentFixture(t)
			params, err := canonical.Bytes([]byte(body))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(params)
			documents[0].Ref.SHA256 = hex.EncodeToString(digest[:])
			documents[0].JCS = string(params)
			var stack map[string]any
			if json.Unmarshal(raw, &stack) != nil {
				t.Fatal("fixture stack")
			}
			stack["layers"].([]any)[0].(map[string]any)["params"] = documents[0].Ref
			raw, err = canonical.Marshal(stack)
			if err != nil {
				t.Fatal(err)
			}
			digest = sha256.Sum256(raw)
			root := documents[0].Parent
			root.SHA256 = hex.EncodeToString(digest[:])
			for i := range documents {
				documents[i].Parent = root
			}
			evidence.Recipe.Recipe, err = canonical.Marshal(map[string]any{"layer_stack": root})
			if err != nil {
				t.Fatal(err)
			}
			validation := ValidateRenderLayerDocuments(evidence, raw, documents)
			if sample.valid && validation != nil {
				t.Fatal("well-formed descendant refused", validation)
			}
			if !sample.valid && !errors.Is(validation, v2input.ErrInvalid) {
				t.Fatal("malformed descendant accepted despite consistent document/root hashes")
			}
			if _, err := loadRenderLayerDocuments(evidence, raw, func(ref v2draft.LayerRef) ([]byte, error) {
				for _, document := range documents {
					if document.Ref == ref.Ref {
						return []byte(document.JCS), nil
					}
				}
				return nil, v2input.ErrInvalid
			}); (sample.valid && err != nil) || (!sample.valid && !errors.Is(err, v2input.ErrInvalid)) {
				t.Fatal("loader accepted malformed descendant", err)
			}
		})
	}
}
