package review

import (
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestProposalFaceMasksCompleteOwnedBindings(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	if err := decodeExact(raw, &report); err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: report.Params.Image, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain}
	manual := *report.Params.Faces[0].Skin
	manual.Selector = "manual"
	report.Params.Faces[0].Manual = &manual
	report.Params.Faces[0].ManualMode = true
	body, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	masks, err := ProposalFaceMasks(body, report.Proposal, source)
	if err != nil || len(masks) != 3 {
		t.Fatal(masks, err)
	}
	if masks[0].Mask.Selector != "skin" || masks[1].Mask.Selector != "manual" || masks[2].Ordinal != 1 || !masks[0].ManualMode || !masks[1].ManualMode {
		t.Fatal("lost inactive skin or face ordering")
	}
	original := *masks[1].ObjectID
	*masks[0].ObjectID = uuid.New()
	*masks[0].Mask.ObjectID = uuid.New()
	masks[0].Mask.TransformChain[0] = 'x'
	if *masks[1].ObjectID != original || *masks[1].Mask.ObjectID != original || masks[1].Mask.TransformChain[0] == 'x' {
		t.Fatal("aliased returned bindings")
	}
	clear(body)
	if *masks[2].ObjectID != *report.Params.Faces[1].ObjectID {
		t.Fatal("input byte alias")
	}
	report.Params.Faces[1].Skin.Artifact.SHA256 = strings.Repeat("a", 64)
	body, _ = canonical.Marshal(report)
	if got, err := ProposalFaceMasks(body, report.Proposal, source); err == nil || got != nil {
		t.Fatal("conflicting shared pixels accepted")
	}
	report.Params.Faces[1].Skin.Artifact.SHA256 = manual.Artifact.SHA256
	report.Params.Faces[1].Skin.Canvas.Width++
	body, _ = canonical.Marshal(report)
	if got, err := ProposalFaceMasks(body, report.Proposal, source); err == nil || got != nil {
		t.Fatal("foreign geometry accepted")
	}
}
