package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func segmentRootFixture(t *testing.T) (CompiledAuthoredSegmentParams, v2input.Ref) {
	t.Helper()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	layers, mask := ref(), ref()
	p := AuthoredSegmentParams{SchemaVersion: 1, TenantID: uuid.New(), ParentRunID: uuid.New(), WorkflowID: uuid.New(), InstructionID: uuid.New(), Ordinal: 2, ProgramSHA256: strings.Repeat("b", 64), PredecessorSHA256: strings.Repeat("c", 64), Recipe: ref(), BaseReceipt: ref(), Source: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), CanvasHash: strings.Repeat("d", 64), Source: ref()}, CurrentLayers: &layers, ProtectedMask: &mask, PhaseReceipts: []v2input.Ref{ref()}}
	c, err := CompileAuthoredSegmentParams(p)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(c.JSON)
	return c, v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
}
func TestAuthoredSegmentInputRootsExactTypedInventory(t *testing.T) {
	c, root := segmentRootFixture(t)
	entries, err := AuthoredSegmentInputRoots(c, root)
	if err != nil || len(entries) != 7 {
		t.Fatal("incomplete direct roots", err)
	}
	want := map[v2input.Ref]string{root: "Params", c.Params.Recipe: "FrozenRenderRecipe", c.Params.BaseReceipt: "AuthoredBaseReceipt", c.Params.Source.Source: "WorkingImage", *c.Params.CurrentLayers: "LayerStack", *c.Params.ProtectedMask: "MaskRef", c.Params.PhaseReceipts[0]: "AuthoredCanonicalPhaseReceipt"}
	for i, entry := range entries {
		if want[entry.Ref] != entry.Contract {
			t.Fatal("unexpected direct input", entry)
		}
		delete(want, entry.Ref)
		if i > 0 && bytes.Compare(entries[i-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0 {
			t.Fatal("noncanonical input order")
		}
	}
	if len(want) != 0 {
		t.Fatal("missing typed roots")
	}
}
func TestAuthoredSegmentInputRootsRejectCachedAndIdentityConflicts(t *testing.T) {
	for _, change := range []string{"wire", "params", "digest", "contract_conflict", "sha_conflict"} {
		t.Run(change, func(t *testing.T) {
			c, root := segmentRootFixture(t)
			switch change {
			case "wire":
				c.JSON = append([]byte(" "), c.JSON...)
			case "params":
				c.Params.InstructionID = uuid.New()
			case "digest":
				root.SHA256 = strings.Repeat("0", 64)
			case "contract_conflict", "sha_conflict":
				ref := c.Params.Recipe
				if change == "sha_conflict" {
					ref.SHA256 = strings.Repeat("f", 64)
				}
				c.Params.CurrentLayers = &ref
				var err error
				c, err = CompileAuthoredSegmentParams(c.Params)
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(c.JSON)
				root.SHA256 = hex.EncodeToString(sum[:])
			}
			if roots, err := AuthoredSegmentInputRoots(c, root); err == nil || roots != nil {
				t.Fatal("forged compiled inputs accepted")
			}
		})
	}
}
