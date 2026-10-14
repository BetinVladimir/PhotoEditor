package interchange

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type remapCurrentSource struct {
	calls, denyAt int
	failure       error
}

func (s *remapCurrentSource) QualifyImportRecipeSource(context.Context, uuid.UUID, v2input.SessionVersionBinding, v2input.SessionVersionBinding) error {
	s.calls++
	if s.denyAt > 0 && s.calls >= s.denyAt {
		return s.failure
	}
	return nil
}
func TestRecursiveImportRemapRechecksSourceBeforePublicationAndResult(t *testing.T) {
	for _, mode := range []string{"valid", "before_publish", "after_publish"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte(`{"name":"child"}`)
			root := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(body)}
			staged := uuid.New()
			parsed := importParsedValidatorFixture()
			parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: root.ArtifactID, Path: "documents/override.json", SHA: root.SHA256, Contract: "Override"})
			parsed.Files = append(parsed.Files, importParsedFile{Path: "documents/override.json", Content: v2input.Ref{ArtifactID: staged, SHA256: root.SHA256}, Contract: "Override"})
			raw, err := canonical.Marshal(parsed)
			if err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("source authority revoked")
			source := &remapCurrentSource{failure: sentinel}
			if mode == "before_publish" {
				source.denyAt = 2
			}
			if mode == "after_publish" {
				source.denyAt = 3
			}
			codec := &dependencyCodecStub{}
			workflow, entry := uuid.New(), parsed.Manifest.Entries[0].ID
			remapper, err := NewRecursiveImportDependencyRemapper(workflow, raw, remapArchiveStub{staged: body}, source, codec, codec)
			if err != nil {
				t.Fatal(err)
			}
			old := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
			target := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
			recipe := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("recipe"))}
			ctx := withImportRecipeConfirmationScope(context.Background(), workflow, entry, recipe, []v2input.Ref{root})
			result, err := remapper.RemapImportRecipeDependency(ctx, entry, root, "Override", old, target)
			if mode == "valid" {
				if err != nil || !validImportRef(result) || source.calls != 3 || len(codec.published) != 1 {
					t.Fatal("current source checks absent", err)
				}
				return
			}
			if !errors.Is(err, sentinel) || result != (v2input.Ref{}) {
				t.Fatal("revoked source returned remap approval", err)
			}
			expected := 0
			if mode == "after_publish" {
				expected = 1
			}
			if len(codec.published) != expected {
				t.Fatal("publication boundary not respected", codec.published)
			}
		})
	}
}
