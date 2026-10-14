package interchange

import (
	"context"
	"sort"
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
		{Ref: scope.MaskPixels, Contract: "MaskPixels"},
	}}
	expected := map[string]PortablePersonOriginExpectation{path: scope}
	if err := ValidateExportPersonOriginSourceClosure(context.Background(), intent, frozen, body, inventory, expected); err != nil {
		t.Fatalf("qualified closure refused: %v", err)
	}
	missing := inventory
	missing.entries = append([]v2input.Entry(nil), inventory.entries[:2]...)
	if err := ValidateExportPersonOriginSourceClosure(context.Background(), intent, frozen, body, missing, expected); err == nil {
		t.Fatal("missing mask pixels accepted")
	}
	foreign := scope
	foreign.Source.AssetVersionID = uuid.New()
	if err := ValidateExportPersonOriginSourceClosure(context.Background(), intent, frozen, body, inventory,
		map[string]PortablePersonOriginExpectation{path: foreign}); err == nil {
		t.Fatal("foreign source binding accepted")
	}
	complete := inventory
	complete.entries = append([]v2input.Entry{{Ref: want.Content, Contract: "EncodedImage"}}, inventory.entries...)
	complete.digest, err = canonical.Hash(complete.entries)
	if err != nil {
		t.Fatal(err)
	}
	receiptID := uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String()))
	receiptSHA := manifest.PersonOrigins[0].SHA
	manifest.Inventory = []exportBundleInventoryEntry{
		{ID: want.Content.ArtifactID, Path: want.Path, SHA: want.Content.SHA256, Contract: "EncodedImage"},
		{ID: scope.Recipe.ArtifactID, Path: "recipes/" + scope.Recipe.ArtifactID.String() + ".json", SHA: scope.Recipe.SHA256, Contract: "EditingRecipeV3"},
		{ID: scope.Override.ArtifactID, Path: "dependencies/" + scope.Override.ArtifactID.String() + ".json", SHA: scope.Override.SHA256, Contract: "Override"},
		{ID: scope.MaskPixels.ArtifactID, Path: "dependencies/" + scope.MaskPixels.ArtifactID.String() + ".bin", SHA: scope.MaskPixels.SHA256, Contract: "MaskPixels"},
		{ID: scope.MaskRef.ArtifactID, Path: "dependencies/" + scope.MaskRef.ArtifactID.String() + ".json", SHA: scope.MaskRef.SHA256, Contract: "MaskRef"},
		{ID: receiptID, Path: path, SHA: receiptSHA, Contract: "PersonOriginReceipt"},
	}
	sort.Slice(manifest.Inventory, func(i, j int) bool { return manifest.Inventory[i].Path < manifest.Inventory[j].Path })
	manifest.Entries = []exportBundleEntry{{ID: want.ID, External: want.External, ImagePath: want.Path,
		ImageSHA: want.Content.SHA256, Rating: want.Rating, Pick: want.Pick, Crop: want.Crop}}
	recipePath := "recipes/" + scope.Recipe.ArtifactID.String() + ".json"
	recipeSHA := scope.Recipe.SHA256
	manifest.Entries[0].RecipePath, manifest.Entries[0].RecipeSHA = &recipePath, &recipeSHA
	manifest.Schema, manifest.Renderer = 1, "v2.2"
	manifest.Masks = []exportBundleMask{{Path: "dependencies/" + scope.MaskPixels.ArtifactID.String() + ".bin",
		SHA: scope.MaskPixels.SHA256, Canvas: scope.Source.CanvasHash}}
	full, err := canonical.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateExportBundleSourceInventoryWithPersonOrigins(context.Background(), intent, frozen, full, complete, expected); err != nil {
		t.Fatalf("qualified portable inventory refused: %v", err)
	}
	if err := ValidateExportBundleSourceInventory(context.Background(), intent, frozen, full, complete); err == nil {
		t.Fatal("legacy validator admitted unverified origins")
	}
	files := []VerifiedExportZIPFile{{Path: "manifest.json", Bytes: int64(len(full)), SHA256: rawSHA(full)}}
	for _, item := range manifest.Inventory {
		files = append(files, VerifiedExportZIPFile{Path: item.Path, Bytes: 2, SHA256: item.SHA})
	}
	if err := ValidateExportManifest(context.Background(), intent, frozen, full, files); err == nil {
		t.Fatal("default manifest validator admitted origins")
	}
	if err := validateExportManifest(context.Background(), intent, frozen, full, files, true); err != nil {
		t.Fatalf("explicit origin manifest gate refused structural bundle: %v", err)
	}
}
