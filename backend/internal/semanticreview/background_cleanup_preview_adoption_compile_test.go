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

func TestCompileShownBackgroundCleanupAdoptionRejectsUnverifiedPhysicalPreview(t *testing.T) {
	c, err := CompileShownBackgroundCleanupAdoption(uuid.New(), ApprovedTaskDraft{}, LockedBackgroundCleanupPreviewSelection{}, PhysicalBackgroundCleanupPreview{})
	if err == nil || c.TaskID != uuid.Nil || c.Body() != nil {
		t.Fatal("unverified preview admitted")
	}
}
func TestCompiledShownBackgroundCleanupAdoptionBodyIsOwned(t *testing.T) {
	c := CompiledShownBackgroundCleanupAdoption{body: []byte("immutable")}
	first := c.Body()
	first[0] = 'x'
	if string(c.Body()) != "immutable" {
		t.Fatal("publication bytes mutated")
	}
}

func shownBackgroundCleanupCompileFixture(t *testing.T) (uuid.UUID, ApprovedTaskDraft, LockedBackgroundCleanupPreviewSelection, PhysicalBackgroundCleanupPreview) {
	t.Helper()
	p := BackgroundCleanupPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind: "background_cleanup", ModelID: "big_lama", ContextPixels: 64, FeatherPixels: 8}
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	run := uuid.New()
	var link BackgroundCleanupPreviewPresentationLinkage
	link.Presentation.Params = p
	link.Presentation.WorkingImage = ref()
	link.Presentation.Pixels = ref()
	link.Presentation.NumericalEvidence = ref()
	link.Document.TaskID = uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+p.TargetID.String()))
	link.Document.ResultID = uuid.New()
	link.Document.Attempt = 2
	link.Document.Fence = 7
	link.Document.Source.Ref = ref()
	_, digest, err := FreezeBackgroundCleanupPreviewParams(p)
	if err != nil {
		t.Fatal(err)
	}
	selected := LockedBackgroundCleanupPreviewSelection{TargetID: p.TargetID, PreviewRunID: run, Receipt: BackgroundCleanupPreviewReceipt{
		SchemaVersion: 1, ParamsSHA: digest, TaskID: link.Document.TaskID, ResultID: link.Document.ResultID, Attempt: 2, Fence: 7,
		Presentation: link.Document.Source.Ref, WorkingImage: link.Presentation.WorkingImage, Pixels: link.Presentation.Pixels, NumericalEvidence: link.Presentation.NumericalEvidence}}
	edit := ApprovedEdit{SchemaVersion: 1, TargetID: p.TargetID, Source: p.Source, Intent: p.Intent, Candidate: p.Candidate,
		Mask: ref(), Transform: ref(), Kind: "background_cleanup", Strength: 1, PreservePersons: true}
	body, err := canonical.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	approvedRef := ref()
	approvedRef.SHA256 = hex.EncodeToString(sum[:])
	draft := ApprovedTaskDraft{TargetID: p.TargetID, Params: approvedRef, Body: body, Kind: "background_cleanup"}
	return uuid.New(), draft, selected, PhysicalBackgroundCleanupPreview{verified: true, linkage: link}
}
func TestShownBackgroundCleanupAdoptionCompilationPreservesSelectedProducerAndSeparateTask(t *testing.T) {
	child, draft, selection, physical := shownBackgroundCleanupCompileFixture(t)
	compiled, err := CompileShownBackgroundCleanupAdoption(child, draft, selection, physical)
	if err != nil {
		t.Fatal(err)
	}
	p := compiled.Params()
	if p.PreviewFence != selection.Receipt.Fence || p.PreviewResultID != selection.Receipt.ResultID || p.Pixels != selection.Receipt.Pixels ||
		p.ChildRunID != child || p.ApprovedEdit != draft.Params || compiled.TaskID != uuid.NewSHA1(child, []byte("semantic.adopt_shown_background_cleanup:"+selection.TargetID.String())) {
		t.Fatal("selected output replaced")
	}
	again, err := CompileShownBackgroundCleanupAdoption(child, draft, selection, physical)
	if err != nil || again.ParamsRef != compiled.ParamsRef || string(again.Body()) != string(compiled.Body()) {
		t.Fatal("unstable compilation")
	}
}
func TestShownBackgroundCleanupAdoptionCompilationRejectsChangedSelection(t *testing.T) {
	for _, change := range []string{"fence", "result", "pixels", "run", "target", "body", "source", "child"} {
		child, draft, selection, physical := shownBackgroundCleanupCompileFixture(t)
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
		got, err := CompileShownBackgroundCleanupAdoption(child, draft, selection, physical)
		if err == nil || got.TaskID != uuid.Nil || got.Body() != nil {
			t.Fatal("changed binding accepted", change)
		}
	}
}

// A matching SHA proves byte integrity, not that exposure-only controls belong
// to a removal approval. Rehash the mutation to exercise semantic validation.
func TestShownBackgroundCleanupAdoptionCompilationRejectsExposureControl(t *testing.T) {
	child, draft, selection, physical := shownBackgroundCleanupCompileFixture(t)
	var edit ApprovedEdit
	if err := decodeStrict(draft.Body, &edit); err != nil {
		t.Fatal(err)
	}
	ev := 1.0
	edit.EV = &ev
	body, err := canonical.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	draft.Body = body
	sum := sha256.Sum256(body)
	draft.Params.SHA256 = hex.EncodeToString(sum[:])
	got, err := CompileShownBackgroundCleanupAdoption(child, draft, selection, physical)
	if err == nil || got.TaskID != uuid.Nil || got.Body() != nil {
		t.Fatal("exposure control admitted into removal copy")
	}
}

func TestShownBackgroundCleanupAdoptionRejectsRehashedForeignApproval(t *testing.T) {
	for _, kind := range []string{"remove_object", "retouch_region", "local_exposure"} {
		t.Run(kind, func(t *testing.T) {
			child, draft, selection, physical := shownBackgroundCleanupCompileFixture(t)
			var edit ApprovedEdit
			if err := decodeStrict(draft.Body, &edit); err != nil {
				t.Fatal(err)
			}
			edit.Kind = kind
			body, err := canonical.Marshal(edit)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(body)
			draft.Body = body
			draft.Params.SHA256 = hex.EncodeToString(sum[:])
			// Keep draft kind as cleanup: rejection must inspect canonical approved bytes.
			got, err := CompileShownBackgroundCleanupAdoption(child, draft, selection, physical)
			if err == nil || got.TaskID != uuid.Nil || got.Body() != nil {
				t.Fatal("foreign approval accepted", kind)
			}
		})
	}
}
