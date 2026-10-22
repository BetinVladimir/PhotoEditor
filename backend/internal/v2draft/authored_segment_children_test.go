package v2draft

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func segmentTraversalFixture(t *testing.T) (map[string]any, v2input.Ref) {
	t.Helper()
	ref := func() map[string]any {
		return map[string]any{"artifact_id": uuid.New().String(), "sha256": strings.Repeat("a", 64)}
	}
	return map[string]any{"schema_version": 1, "tenant_id": uuid.New().String(), "parent_run_id": uuid.New().String(), "workflow_id": uuid.New().String(), "instruction_id": uuid.New().String(), "ordinal": 2, "program_sha256": strings.Repeat("b", 64), "predecessor_sha256": strings.Repeat("c", 64), "recipe": ref(), "base_receipt": ref(), "source": map[string]any{"asset_version_id": uuid.New().String(), "canvas_hash": strings.Repeat("d", 64), "source": ref()}, "current_layers": ref(), "protected_mask": ref(), "phase_receipts": []any{ref()}}, v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
}
func TestAuthoredSegmentTraversalTypedOccurrenceOrder(t *testing.T) {
	f, parent := segmentTraversalFixture(t)
	raw, err := canonical.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{"AuthoredSegmentParams", "Params"} {
		refs, err := DocumentChildren(raw, contract, parent)
		if err != nil {
			t.Fatal(err)
		}
		paths := []string{".recipe", ".base_receipt", ".source.source", ".current_layers", ".protected_mask", ".phase_receipts[0]"}
		contracts := []string{"FrozenRenderRecipe", "AuthoredBaseReceipt", "WorkingImage", "LayerStack", "MaskRef", "AuthoredCanonicalPhaseReceipt"}
		if len(refs) != len(paths) {
			t.Fatal("incomplete inventory")
		}
		for i, r := range refs {
			if r.Path != "Document"+paths[i] || r.Contract != contracts[i] || r.ParentRef != parent {
				t.Fatal("wrong typed occurrence", i, r)
			}
		}
	}
}
func TestAuthoredSegmentTraversalRequiredNullableFirstOccurrence(t *testing.T) {
	f, parent := segmentTraversalFixture(t)
	f["ordinal"] = 1
	f["current_layers"] = nil
	f["protected_mask"] = nil
	f["phase_receipts"] = []any{}
	raw, _ := canonical.Marshal(f)
	refs, err := DocumentChildren(raw, "Params", parent)
	if err != nil || len(refs) != 3 {
		t.Fatal("explicit null first occurrence refused", err)
	}
}
func TestAuthoredSegmentTraversalRefusesMalformedBeforeOpaqueFallback(t *testing.T) {
	for _, change := range []string{"missing_layers", "missing_predecessor", "null_receipts", "duplicate_receipt", "ordinal_mismatch", "bool_ordinal", "zero_tenant", "unknown_field", "invalid_layer_ref", "noncanonical"} {
		t.Run(change, func(t *testing.T) {
			f, parent := segmentTraversalFixture(t)
			switch change {
			case "missing_layers":
				delete(f, "current_layers")
			case "missing_predecessor":
				delete(f, "predecessor_sha256")
			case "null_receipts":
				f["phase_receipts"] = nil
			case "duplicate_receipt":
				r := f["phase_receipts"].([]any)[0]
				f["phase_receipts"] = []any{r, r}
				f["ordinal"] = 3
			case "ordinal_mismatch":
				f["ordinal"] = 1
			case "bool_ordinal":
				f["ordinal"] = true
			case "zero_tenant":
				f["tenant_id"] = uuid.Nil.String()
			case "unknown_field":
				f["untrusted"] = 1
			case "invalid_layer_ref":
				f["current_layers"] = map[string]any{"artifact_id": uuid.New().String(), "sha256": "bad"}
			}
			raw, _ := canonical.Marshal(f)
			if change == "noncanonical" {
				raw = append([]byte(" "), raw...)
			}
			for _, contract := range []string{"Params", "AuthoredSegmentParams"} {
				if refs, err := DocumentChildren(raw, contract, parent); err == nil || refs != nil {
					t.Fatal("malformed document escaped strict traversal", contract)
				}
			}
		})
	}
	// The discriminator does not intercept unrelated ordinary Params.
	raw, _ := json.Marshal(map[string]any{"image": map[string]any{"artifact_id": uuid.New().String(), "sha256": strings.Repeat("a", 64)}})
	if _, handled, err := authoredSegmentParamRefs(raw, "Document", v2input.Ref{}); handled || err != nil {
		t.Fatal("ordinary params intercepted")
	}
}
