package interchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type stagedPersonScopeSource map[uuid.UUID]objectstore.Object

func (s stagedPersonScopeSource) LookupPersonOriginReceipt(_ context.Context, _ []byte, ref v2input.Ref) (objectstore.Object, error) {
	object, ok := s[ref.ArtifactID]
	if !ok {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return object, nil
}

func TestStagedPersonOriginScopeRequiresPhysicalRecipeChain(t *testing.T) {
	_, _, sourceRaw := importWorkingValidatorFixture(t)
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	model := rawSHA([]byte("model"))
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(sourceRaw)}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: canvas}
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain,
		Selector: "person", Confidence: 1, ModelLockID: &model}
	maskRaw, err := canonical.Marshal(mask)
	if err != nil {
		t.Fatal(err)
	}
	maskRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(maskRaw)}
	pixels := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
	overrideRaw, err := canonical.Marshal(map[string]any{"schema_version": 1, "input_sha256": image.Canvas.SourceSHA256,
		"modules": map[string]any{"strength": .7}, "revision": 1,
		"mask_refs": []any{map[string]any{"binding_id": "person-1", "purpose": "person_subject",
			"mask": json.RawMessage(maskRaw), "confirmed": true, "strength": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	overrideRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(overrideRaw)}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("style"))}
	recipeRaw, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style,
		"session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{overrideRef},
		"layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}})
	if err != nil {
		t.Fatal(err)
	}
	recipeRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(recipeRaw)}
	expected := PortablePersonOriginExpectation{EntryID: uuid.New(), Source: binding, Recipe: recipeRef,
		Override: overrideRef, MaskRef: maskRef, MaskPixels: pixels, ModelLockSHA: model}
	document := importParsedValidatorFixture()
	document.Manifest.Entries[0].ID = expected.EntryID
	objects := personOriginRecordObjects{}
	locator := stagedPersonScopeSource{}
	add := func(ref v2input.Ref, contract string, raw []byte) string {
		path := "dependencies/" + ref.ArtifactID.String() + ".json"
		staged := v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}
		document.Manifest.Inventory = append(document.Manifest.Inventory,
			importBundleInventory{ID: ref.ArtifactID, Path: path, SHA: ref.SHA256, Contract: contract})
		document.Files = append(document.Files, importParsedFile{Path: path, Content: staged, Contract: contract})
		key := ref.ArtifactID.String()
		objects[key] = raw
		locator[staged.ArtifactID] = objectstore.Object{Key: key, VersionID: "v1", SHA256: ref.SHA256,
			Bytes: int64(len(raw)), ContentType: "application/octet-stream"}
		return path
	}
	recipePath := add(recipeRef, "EditingRecipeV3", recipeRaw)
	recipeSHA := recipeRef.SHA256
	document.Manifest.Entries[0].RecipePath = &recipePath
	document.Manifest.Entries[0].RecipeSHA = &recipeSHA
	add(source, "WorkingImage", sourceRaw)
	add(overrideRef, "Override", overrideRaw)
	add(maskRef, "MaskRef", maskRaw)
	add(pixels, "MaskPixels", []byte("pixels"))
	parsed, err := canonical.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	authority := StagedImportPersonOriginScope{Source: locator, Objects: objects}
	if err := authority.QualifyImportedPersonOriginScope(context.Background(), parsed, expected); err != nil {
		t.Fatal("exact staged person occurrence rejected", err)
	}
	changed := expected
	changed.Override.ArtifactID = uuid.New()
	if err := authority.QualifyImportedPersonOriginScope(context.Background(), parsed, changed); err == nil {
		t.Fatal("foreign override admitted")
	}
	objects[maskRef.ArtifactID.String()] = []byte(`{}`)
	if err := authority.QualifyImportedPersonOriginScope(context.Background(), parsed, expected); err == nil {
		t.Fatal("substituted MaskRef bytes admitted")
	}
}
