package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestExportPersonOriginRequiresFrozenEntryAndQualifiedSourceClosure(t *testing.T) {
	intent, compiled, snapshot, snapshotRaw := exportFreezeFixture(t)
	intent.Request.Format = "photoeditor_bundle"
	intent.RequestJCS, _ = canonical.Marshal(intent.Request)
	intent.RequestSHA = rawSHA(intent.RequestJCS)
	var prior importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &prior); err != nil {
		t.Fatal(err)
	}
	entry := prior.Entries[0]
	var err error
	compiled, err = CompileExportPlan(context.Background(), intent.Identity.Workflow,
		intent.Request.ProjectID, "photoeditor_bundle", snapshot, snapshotRaw,
		ExportLimits{1 << 20, 1 << 20, 10}, exportFrameResolverFunc(func(context.Context, v2input.SessionSnapshotItem) (ExportFrame, error) {
			return ExportFrame{ExternalID: entry.External, Path: entry.Path, Content: entry.Content,
				PortableRecipe: *entry.Recipe, Rating: entry.Rating, Pick: entry.Pick, Crop: entry.Crop}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := FreezeExportDocuments(context.Background(), intent, compiled, snapshot, snapshotRaw)
	if err != nil {
		t.Fatal(err)
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		t.Fatal(err)
	}
	want := plan.Entries[0]
	if want.Binding == nil || want.Recipe == nil {
		t.Fatal("fixture lacks bound recipe")
	}
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(uuid.NewString()))}
	}
	scope := PortablePersonOriginExpectation{EntryID: want.ID, Source: *want.Binding,
		Recipe: *want.Recipe, Override: ref(), MaskRef: ref(), MaskPixels: ref(),
		ModelLockSHA: rawSHA([]byte("model lock"))}
	path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
	manifest := exportBundleManifest{PersonOrigins: []importBundlePersonOrigin{{EntryID: scope.EntryID,
		Recipe: scope.Recipe, Override: scope.Override, MaskRef: scope.MaskRef,
		MaskPixels: scope.MaskPixels, Path: path, SHA: rawSHA([]byte("receipt"))}}}
	body, err := canonical.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	inventory := ExportInputInventory{task: compiled.task, entries: []v2input.Entry{
		{Ref: scope.Recipe, Contract: "EditingRecipeV3"},
		{Ref: scope.Override, Contract: "Override"},
		{Ref: scope.MaskRef, Contract: "MaskRef"},
		{Ref: scope.MaskPixels, Contract: "MaskPixels"},
	}}
	expected := map[string]PortablePersonOriginExpectation{path: scope}
	if err := ValidateExportPersonOriginSourceClosure(context.Background(), intent, frozen, body, inventory, expected); err != nil {
		t.Fatalf("qualified closure refused: %v", err)
	}
	missing := inventory
	missing.entries = append([]v2input.Entry(nil), inventory.entries[:3]...)
	if err := ValidateExportPersonOriginSourceClosure(context.Background(), intent, frozen, body, missing, expected); err == nil {
		t.Fatal("missing mask pixels accepted")
	}
	foreign := scope
	foreign.Source.AssetVersionID = uuid.New()
	if err := ValidateExportPersonOriginSourceClosure(context.Background(), intent, frozen, body, inventory,
		map[string]PortablePersonOriginExpectation{path: foreign}); err == nil {
		t.Fatal("foreign source binding accepted")
	}
}
