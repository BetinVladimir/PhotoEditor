package interchange

import (
	"context"
	"github.com/google/uuid"
	"path"
	"photoeditor/backend/internal/canonical"
	"sort"
	"strings"
	"testing"
)

func TestExportManifestBindsSidecarInventoryAndFrozenEntries(t *testing.T) {
	intent, compiled, snapshot, raw := exportFreezeFixture(t)
	frozen, err := FreezeExportDocuments(context.Background(), intent, compiled, snapshot, raw)
	if err != nil {
		t.Fatal(err)
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		t.Fatal(err)
	}
	want := plan.Entries[0]
	sidecarPath := strings.TrimSuffix(want.Path, path.Ext(want.Path)) + ".xmp"
	sidecarSHA := rawSHA([]byte("xmp"))
	for _, kind := range []string{"valid", "image_sha", "image_id", "sidecar_id", "sidecar_contract", "external", "rating", "unplanned_label", "recipe", "extra_file", "missing_file", "manifest_sha", "mask"} {
		t.Run(kind, func(t *testing.T) {
			manifest := exportBundleManifest{Schema: 1, Renderer: "v2.2", Entries: []exportBundleEntry{{ID: want.ID, External: want.External, ImagePath: want.Path, ImageSHA: want.Content.SHA256, Rating: want.Rating, Pick: want.Pick, Crop: want.Crop}}, Masks: []exportBundleMask{}, Inventory: []exportBundleInventoryEntry{{ID: want.Content.ArtifactID, Path: want.Path, SHA: want.Content.SHA256, Contract: "EncodedImage"}, {ID: uuid.NewSHA1(want.ID, []byte("interchange:xmp:"+sidecarSHA)), Path: sidecarPath, SHA: sidecarSHA, Contract: "XMP"}}}
			switch kind {
			case "image_sha":
				manifest.Entries[0].ImageSHA = sidecarSHA
			case "image_id":
				manifest.Inventory[0].ID = uuid.New()
			case "sidecar_id":
				manifest.Inventory[1].ID = uuid.New()
			case "sidecar_contract":
				manifest.Inventory[1].Contract = "Params"
			case "external":
				manifest.Entries[0].External = "wrong"
			case "rating":
				manifest.Entries[0].Rating = 99
			case "unplanned_label":
				value := "Blue"
				manifest.Entries[0].Label = &value
			case "recipe":
				value := "recipe.json"
				manifest.Entries[0].RecipePath = &value
			case "mask":
				manifest.Masks = []exportBundleMask{{Path: sidecarPath, SHA: sidecarSHA, Canvas: sidecarSHA}}
			}
			sort.Slice(manifest.Inventory, func(i, j int) bool { return manifest.Inventory[i].Path < manifest.Inventory[j].Path })
			body, err := canonical.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			files := []VerifiedExportZIPFile{{Path: want.Path, Bytes: 12, SHA256: want.Content.SHA256}, {Path: sidecarPath, Bytes: 3, SHA256: sidecarSHA}, {Path: "manifest.json", Bytes: int64(len(body)), SHA256: rawSHA(body)}}
			switch kind {
			case "extra_file":
				files = append(files, VerifiedExportZIPFile{Path: "unexpected.bin", Bytes: 1, SHA256: sidecarSHA})
			case "missing_file":
				files = files[1:]
			case "manifest_sha":
				files[2].SHA256 = sidecarSHA
			}
			err = ValidateExportManifest(context.Background(), intent, frozen, body, files)
			if kind == "valid" {
				if err != nil {
					t.Fatal("valid manifest refused", err)
				}
			} else if err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateExportManifest(ctx, intent, frozen, nil, nil); err == nil {
		t.Fatal("cancel accepted")
	}
}
