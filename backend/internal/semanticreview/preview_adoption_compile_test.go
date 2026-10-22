package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestCompileShownAdoptionRejectsUnverifiedPhysicalPreview(t *testing.T) {
	c, err := CompileShownExposureAdoption(uuid.New(), ApprovedTaskDraft{}, LockedPreviewSelection{}, PhysicalExposurePreview{})
	if err == nil || c.TaskID != uuid.Nil || c.Body() != nil {
		t.Fatal("unverified preview admitted")
	}
}
func TestCompiledShownAdoptionBodyIsOwned(t *testing.T) {
	c := CompiledShownExposureAdoption{body: []byte("immutable")}
	first := c.Body()
	first[0] = 'x'
	if string(c.Body()) != "immutable" {
		t.Fatal("publication bytes mutated")
	}
}

func shownCompileFixture(t *testing.T) (uuid.UUID, ApprovedTaskDraft, LockedPreviewSelection, PhysicalExposurePreview) {
	t.Helper()
	p := previewParamsFixture()
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	run := uuid.New()
	var link PreviewPresentationLinkage
	link.Presentation.Params = p
	link.Presentation.WorkingImage = ref()
	link.Presentation.Pixels = ref()
	link.Presentation.NumericalEvidence = ref()
	link.Document.TaskID = uuid.NewSHA1(run, []byte("semantic.preview_local_exposure:"+p.TargetID.String()))
	link.Document.ResultID = uuid.New()
	link.Document.Attempt = 2
	link.Document.Fence = 7
	link.Document.Source.Ref = ref()
	_, digest, err := FreezeExposurePreviewParams(p)
	if err != nil {
		t.Fatal(err)
	}
	selected := LockedPreviewSelection{TargetID: p.TargetID, PreviewRunID: run, Receipt: ExposurePreviewReceipt{
		SchemaVersion: 1, ParamsSHA: digest, TaskID: link.Document.TaskID, ResultID: link.Document.ResultID, Attempt: 2, Fence: 7,
		Presentation: link.Document.Source.Ref, WorkingImage: link.Presentation.WorkingImage, Pixels: link.Presentation.Pixels, NumericalEvidence: link.Presentation.NumericalEvidence}}
	ev := 1.0
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: p.TargetID, Source: p.Source, Intent: p.Intent, Candidate: p.Candidate,
		Mask: ref(), Transform: ref(), Kind: "local_exposure", Strength: 1, EV: &ev, PreservePersons: true}
	body, err := canonical.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	approvedRef := ref()
	approvedRef.SHA256 = hex.EncodeToString(sum[:])
	draft := ApprovedTaskDraft{TargetID: p.TargetID, Params: approvedRef, Body: body, Kind: "local_exposure"}
	return uuid.New(), draft, selected, PhysicalExposurePreview{verified: true, linkage: link}
}
func TestShownAdoptionCompilationPreservesSelectedProducerAndSeparateTask(t *testing.T) {
	child, draft, selection, physical := shownCompileFixture(t)
	compiled, err := CompileShownExposureAdoption(child, draft, selection, physical)
	if err != nil {
		t.Fatal(err)
	}
	p := compiled.Params()
	if p.PreviewFence != selection.Receipt.Fence || p.PreviewResultID != selection.Receipt.ResultID || p.Pixels != selection.Receipt.Pixels ||
		p.ChildRunID != child || p.ApprovedEdit != draft.Params || compiled.TaskID != uuid.NewSHA1(child, []byte("semantic.adopt_shown_exposure:"+selection.TargetID.String())) {
		t.Fatal("selected output replaced")
	}
	again, err := CompileShownExposureAdoption(child, draft, selection, physical)
	if err != nil || again.ParamsRef != compiled.ParamsRef || string(again.Body()) != string(compiled.Body()) {
		t.Fatal("unstable compilation")
	}
}
func TestShownAdoptionCompilationRejectsChangedSelection(t *testing.T) {
	for _, change := range []string{"fence", "result", "pixels", "run", "target", "body", "source", "child"} {
		child, draft, selection, physical := shownCompileFixture(t)
		switch change {
		case "fence":
			selection.Receipt.Fence++
		case "result":
			selection.Receipt.ResultID = uuid.New()
		case "pixels":
			selection.Receipt.Pixels.ArtifactID = uuid.New()
		case "run":
			selection.PreviewRunID = uuid.New()
		case "target":
			selection.TargetID = uuid.New()
		case "body":
			draft.Body = append(draft.Body, ' ')
		case "source":
			physical.linkage.Presentation.Params.Source.AssetVersionID = uuid.New()
		case "child":
			child = selection.PreviewRunID
		}
		got, err := CompileShownExposureAdoption(child, draft, selection, physical)
		if err == nil || got.TaskID != uuid.Nil || got.Body() != nil {
			t.Fatal("changed binding accepted", change)
		}
	}
}
