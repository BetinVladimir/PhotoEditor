package review

import (
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestOriginalProposalAdditionalProtections(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var document proposalReportDocument
	if err := decodeExact(raw, &document); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: document.Params.Image, Canvas: document.Protection.Canvas, TransformChain: document.Protection.TransformChain}
	required := document.Protection
	required.Artifact.ID = uuid.New()
	initial := []ProtectionMask{required}
	document.Params.Protections = &initial
	body, err := canonical.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProposalProtection(body, document.Proposal, source); err != nil {
		t.Fatal("original protections", err)
	}
	empty := []ProtectionMask{}
	emptyDocument := document
	emptyDocument.Params.Protections = &empty
	emptyBody, err := canonical.Marshal(emptyDocument)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProposalProtection(emptyBody, document.Proposal, source); err != nil {
		t.Fatal("explicit empty protections", err)
	}
	for _, mode := range []string{"unprotected", "geometry", "duplicate", "permanent duplicate", "limit"} {
		changed := document
		mask := required
		switch mode {
		case "unprotected":
			mask.Protected = false
			items := []ProtectionMask{mask}
			changed.Params.Protections = &items
		case "geometry":
			mask.Canvas.Width++
			items := []ProtectionMask{mask}
			changed.Params.Protections = &items
		case "duplicate":
			items := []ProtectionMask{mask, mask}
			changed.Params.Protections = &items
		case "permanent duplicate":
			changed.Params.Permanent = &mask
		case "limit":
			items := make([]ProtectionMask, 65)
			changed.Params.Protections = &items
		}
		bad, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseProposalProtection(bad, document.Proposal, source); err == nil {
			t.Fatal("invalid protection input accepted", mode)
		}
	}
}
