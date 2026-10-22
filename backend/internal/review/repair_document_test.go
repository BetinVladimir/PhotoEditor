package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func repairFixture(t *testing.T) (VerifiedProposal, DefectSelection, ArtifactRef, ArtifactRef, ProtectionMask, []byte) {
	t.Helper()
	raw, ref, source := proposalFixture(t)
	proposal, err := ReadDefectProposal(context.Background(), bytes.NewReader(raw), ref, source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("testdata/defect-selection-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var selection DefectSelection
	if err = json.Unmarshal(raw, &selection); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("testdata/defect-repair-params-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var params struct {
		Review    ArtifactRef    `json:"review"`
		Resolved  ArtifactRef    `json:"resolved"`
		Protected ProtectionMask `json:"protected"`
	}
	if err = json.Unmarshal(raw, &params); err != nil {
		t.Fatal(err)
	}
	return proposal, selection, params.Review, params.Resolved, params.Protected, raw
}
func TestRepairDocumentPythonParityAndDeclaredInputs(t *testing.T) {
	p, s, r, resolved, mask, want := repairFixture(t)
	alpha := ArtifactRef{ID: uuid.MustParse("00000000-0000-0000-0000-000000000008"), SHA256: p.document.SourcePixels.SHA256}
	doc, err := BuildRepairDocument(p, s, r, resolved, p.document.Image, mask, &alpha)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(doc.Params, want) {
		t.Fatal("Go params differ from Python contract")
	}
	if len(doc.Inputs) != 8 {
		t.Fatal("missing declared inputs", doc.Inputs)
	}
	refs := map[ArtifactRef]bool{}
	for i, ref := range doc.Inputs {
		refs[ref] = true
		if i > 0 && doc.Inputs[i-1].ID.String() >= ref.ID.String() {
			t.Fatal("input order not deterministic")
		}
	}
	for _, ref := range []ArtifactRef{p.ref, p.document.Image, p.document.SourcePixels, p.document.LabelMap, r, resolved, mask.Artifact, alpha} {
		if !refs[ref] {
			t.Fatal("missing ref", ref)
		}
	}
}
func TestRepairDocumentRejectsUnboundSelections(t *testing.T) {
	for _, variant := range []string{"sha", "proposal", "unknown", "duplicate", "conflict", "empty", "mask", "geometry", "colliding-ref", "nil-rejected", "revision-one", "revision-overflow", "invalid-id"} {
		t.Run(variant, func(t *testing.T) {
			p, s, r, resolved, mask, _ := repairFixture(t)
			switch variant {
			case "sha":
				r.SHA256 = p.ref.SHA256
			case "proposal":
				s.Proposal.ID = uuid.New()
			case "unknown":
				s.Accepted = []string{p.ref.SHA256}
			case "duplicate":
				s.Accepted = append(s.Accepted, s.Accepted[0])
			case "conflict":
				s.Rejected = append(s.Rejected, s.Accepted[0])
			case "empty":
				s.Accepted = []string{}
			case "mask":
				mask.Protected = false
			case "geometry":
				mask.Canvas.Width++
			case "nil-rejected":
				s.Rejected = nil
			case "revision-one":
				s.Revision = 1
			case "revision-overflow":
				s.Revision = maxRevision + 1
			case "invalid-id":
				s.Accepted = []string{"not-a-hash"}
			case "colliding-ref":
				resolved.ID = p.ref.ID
			}
			if variant != "sha" && variant != "revision-overflow" {
				r.SHA256 = selectionDigest(t, s)
			}
			if _, err := BuildRepairDocument(p, s, r, resolved, p.document.Image, mask, nil); err == nil {
				t.Fatal("invalid repair accepted")
			}
		})
	}
}
func selectionDigest(t *testing.T, s DefectSelection) string {
	t.Helper()
	raw, err := canonical.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
