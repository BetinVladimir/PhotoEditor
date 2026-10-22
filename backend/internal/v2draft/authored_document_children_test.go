package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func authoredVector(t *testing.T, name string) []byte {
	t.Helper()
	raw, e := os.ReadFile("testdata/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func authoredParent() v2input.Ref {
	return v2input.Ref{ArtifactID: uuid.MustParse("10000000-0000-0000-0000-000000000001"), SHA256: strings.Repeat("b", 64)}
}
func TestAuthoredDocumentChildrenExactTypedAndOpaqueOccurrences(t *testing.T) {
	cases := map[string][]string{
		"AuthoredBaseReceipt":           {"FrozenRenderRecipe", "WorkingImage", "WorkingImage", "StyleV2", "Params", "Artifact", "QualityReport"},
		"AuthoredCanonicalPhaseReceipt": {"Params", "Artifact", "FrozenRenderRecipe", "WorkingImage", "WorkingImage", "ImageResult", "ImageReport", "Artifact", "Artifact", "Artifact"},
		"AuthoredCompoundContinuation":  {"FrozenRenderRecipe", "WorkingImage", "AuthoredBaseReceipt", "AuthoredGeometryPhaseResult", "AuthoredCanonicalPhaseReceipt", "WorkingImage", "WorkingImage"},
		"AuthoredGeometryPhaseResult":   {"FrozenRenderRecipe", "WorkingImage", "WorkingImage", "Params", "ImageResult", "WorkingImage", "ImageReport", "Params", "WorkingImage", "WorkingImage", "WorkingImage", "ImageReport", "MaskRef", "WorkingImage", "ImageResult", "WorkingImage"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			raw := authoredVector(t, name)
			before := append([]byte(nil), raw...)
			children, e := DocumentChildren(raw, name, authoredParent())
			if e != nil {
				t.Fatal(e)
			}
			if len(children) != len(want) {
				t.Fatalf("children=%d want=%d: %#v", len(children), len(want), children)
			}
			paths := map[string]bool{}
			for i, child := range children {
				if child.Contract != want[i] || child.ParentRef != authoredParent() || !strings.HasPrefix(child.Path, "Document.") || paths[child.Path] {
					t.Fatalf("invalid typed occurrence %d: %#v", i, child)
				}
				paths[child.Path] = true
			}
			if !bytes.Equal(raw, before) {
				t.Fatal("input mutated")
			}
			// Returned occurrences own their values; a caller cannot poison replay.
			children[0].Ref.SHA256 = strings.Repeat("0", 64)
			again, e := DocumentChildren(raw, name, authoredParent())
			if e != nil || again[0].Ref.SHA256 == children[0].Ref.SHA256 {
				t.Fatal("mutable cached child")
			}
		})
	}
}
func TestAuthoredDocumentsRejectUnknownMissingNullAndNoncanonical(t *testing.T) {
	for _, name := range []string{"AuthoredBaseReceipt", "AuthoredCanonicalPhaseReceipt", "AuthoredCompoundContinuation", "AuthoredGeometryPhaseResult"} {
		t.Run(name, func(t *testing.T) {
			raw := authoredVector(t, name)
			var doc map[string]json.RawMessage
			if json.Unmarshal(raw, &doc) != nil {
				t.Fatal("fixture")
			}
			for _, mode := range []string{"unknown", "missing", "null", "version", "noncanonical", "duplicate"} {
				t.Run(mode, func(t *testing.T) {
					var copy map[string]json.RawMessage
					json.Unmarshal(raw, &copy)
					switch mode {
					case "unknown":
						copy["unexpected"] = json.RawMessage(`{"artifact_id":"10000000-0000-0000-0000-000000000002","sha256":"` + strings.Repeat("a", 64) + `"}`)
					case "missing":
						delete(copy, "tenant_id")
					case "null":
						copy["tenant_id"] = json.RawMessage(`null`)
					case "version":
						copy["schema_version"] = json.RawMessage(`99`)
					}
					bad, e := canonical.Marshal(copy)
					if e != nil {
						t.Fatal(e)
					}
					if mode == "noncanonical" {
						bad = append([]byte(" "), bad...)
					}
					if mode == "duplicate" {
						bad = append([]byte(`{"tenant_id":"10000000-0000-0000-0000-000000000001",`), raw[1:]...)
					}
					if _, e = DocumentChildren(bad, name, authoredParent()); e == nil {
						t.Fatal("malformed document exposed children")
					}
				})
			}
		})
	}
}
func TestAuthoredTranscriptJCSDoesNotBecomeArbitraryInputDiscovery(t *testing.T) {
	raw := authoredVector(t, "AuthoredCanonicalPhaseReceipt")
	var doc map[string]any
	json.Unmarshal(raw, &doc)
	hidden := v2input.Ref{ArtifactID: uuid.MustParse("20000000-0000-0000-0000-000000000001"), SHA256: strings.Repeat("f", 64)}
	profile, e := canonical.Marshal(map[string]any{"nested": hidden})
	if e != nil {
		t.Fatal(e)
	}
	doc["execution_profile_jcs"] = string(profile)
	hash, e := canonical.Hash(map[string]any{"nested": hidden})
	if e != nil {
		t.Fatal(e)
	}
	doc["execution_profile_sha256"] = hash
	encoded, e := canonical.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	children, e := DocumentChildren(encoded, "AuthoredCanonicalPhaseReceipt", authoredParent())
	if e != nil {
		t.Fatal(e)
	}
	for _, child := range children {
		if child.Ref == hidden {
			t.Fatal("provenance string authorized an arbitrary child")
		}
	}
}
func TestAuthoredPhaseAndNativeBranchContracts(t *testing.T) {
	for _, tc := range []struct{ file, contract, resultPath, resultContract string }{{"phase-geometry", "AuthoredCanonicalPhaseReceipt", "Document.result", "AuthoredGeometryPhaseResult"}, {"phase-finalize", "AuthoredCanonicalPhaseReceipt", "Document.result", "ImageResult"}, {"geometry-upscale", "AuthoredGeometryPhaseResult", "Document.native.root", "WorkingImage"}} {
		t.Run(tc.file, func(t *testing.T) {
			children, e := DocumentChildren(authoredVector(t, tc.file), tc.contract, authoredParent())
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, child := range children {
				if child.Path == tc.resultPath {
					found = true
					if child.Contract != tc.resultContract {
						t.Fatal("wrong operation-specific typed root", child)
					}
				}
			}
			if !found {
				t.Fatal("required root omitted")
			}
		})
	}
}
