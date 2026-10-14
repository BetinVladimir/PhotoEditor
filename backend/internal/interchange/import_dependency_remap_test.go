package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type remapArchiveStub map[uuid.UUID][]byte

func (s remapArchiveStub) ReadImportArchiveDocument(_ context.Context, ref v2input.Ref, limit int64) ([]byte, error) {
	body := s[ref.ArtifactID]
	if int64(len(body)) > limit {
		return nil, v2input.ErrInvalid
	}
	return body, nil
}

type dependencyCodecStub struct {
	root, child v2input.Ref
	failure     string
	calls       int
	published   []string
	sentinel    error
}

func (s *dependencyCodecStub) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	s.calls++
	if kind == "Override" {
		scope, err := confirmedOverrideScope(ctx, raw)
		if err != nil || s.child != (v2input.Ref{}) && scope.Override != s.child || scope.Entry == uuid.Nil || scope.Workflow == uuid.Nil || !validImportRef(scope.Recipe) {
			return nil, v2input.ErrInvalid
		}
	}
	if kind != "Override" {
		if _, err := confirmedOverrideScope(ctx, raw); err == nil {
			return nil, v2input.ErrInvalid
		}
	}
	if s.failure == "cycle" {
		_, err := resolve(s.root, "StyleV2")
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value["name"] == "parent" {
		childKind := "Override"
		if s.failure == "type" {
			childKind = "LayerStack"
		}
		a, err := resolve(s.child, childKind)
		if err != nil {
			return nil, err
		}
		b, err := resolve(s.child, childKind)
		if err != nil {
			return nil, err
		}
		value["children"] = []v2input.Ref{a, b}
	}
	value["binding"] = target
	return canonical.Marshal(value)
}
func (s *dependencyCodecStub) QualifyRemappedImportDocument(_ context.Context, _ string, raw []byte, _ v2input.SessionVersionBinding) error {
	if s.failure == "qualification" {
		return s.sentinel
	}
	raw[0] = 'x'
	return nil
}
func (s *dependencyCodecStub) PublishImportDependency(_ context.Context, _ uuid.UUID, kind string, ref v2input.Ref, raw []byte) (v2input.Ref, error) {
	if rawSHA(raw) != ref.SHA256 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	s.published = append(s.published, kind)
	if s.failure == "receipt" {
		ref.SHA256 = rawSHA([]byte("foreign"))
	}
	return ref, nil
}

func TestRecursiveImportRemapperPublishesSharedDependenciesBeforeParent(t *testing.T) {
	for _, failure := range []string{"none", "cycle", "type", "hash", "qualification", "receipt"} {
		t.Run(failure, func(t *testing.T) {
			parentRaw, childRaw := []byte(`{"name":"parent"}`), []byte(`{"name":"child"}`)
			parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(parentRaw)}
			child := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(childRaw)}
			parentStaged, childStaged := uuid.New(), uuid.New()
			parsed := importParsedValidatorFixture()
			for _, item := range []struct {
				ref        v2input.Ref
				staged     uuid.UUID
				path, kind string
			}{{parent, parentStaged, "documents/style.json", "StyleV2"}, {child, childStaged, "documents/override.json", "Override"}} {
				parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: item.ref.ArtifactID, Path: item.path, SHA: item.ref.SHA256, Contract: item.kind})
				parsed.Files = append(parsed.Files, importParsedFile{Path: item.path, Content: v2input.Ref{ArtifactID: item.staged, SHA256: item.ref.SHA256}, Contract: item.kind})
			}
			parsedRaw, err := canonical.Marshal(parsed)
			if err != nil {
				t.Fatal(err)
			}
			old := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
			target := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
			source := &importRecipePorts{old: old, target: target}
			archive := remapArchiveStub{parentStaged: parentRaw, childStaged: childRaw}
			if failure == "hash" {
				archive[parentStaged] = []byte(`{}`)
			}
			codec := &dependencyCodecStub{root: parent, child: child, failure: failure, sentinel: errors.New("unqualified dependency")}
			workflow, entry := uuid.New(), parsed.Manifest.Entries[0].ID
			remapper, err := NewRecursiveImportDependencyRemapper(workflow, parsedRaw, archive, source, codec, codec)
			if err != nil {
				t.Fatal(err)
			}
			recipe := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("recipe"))}
			ctx := withImportRecipeConfirmationScope(context.Background(), workflow, entry, recipe, []v2input.Ref{child})
			result, err := remapper.RemapImportRecipeDependency(ctx, entry, parent, "StyleV2", old, target)
			if failure != "none" {
				if err == nil {
					t.Fatal("invalid graph accepted")
				}
				if failure == "qualification" && !errors.Is(err, codec.sentinel) {
					t.Fatal("qualification failure lost", err)
				}
				return
			}
			if err != nil || result.ArtifactID == parent.ArtifactID || !validImportRef(result) {
				t.Fatal("remap", err)
			}
			if codec.calls != 2 || len(codec.published) != 2 || codec.published[0] != "Override" || codec.published[1] != "StyleV2" {
				t.Fatal("shared dependency duplicated or parent published first", codec.calls, codec.published)
			}
			again, err := remapper.RemapImportRecipeDependency(ctx, entry, parent, "StyleV2", old, target)
			if err != nil || again != result {
				t.Fatal("replay changed immutable identity", err)
			}
		})
	}
}

func TestRecursiveImportRemapperRoutesAbsoluteLUTArrayToBinaryCodec(t *testing.T) {
	archived := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("binary NPY fixture"))}
	staged := uuid.New()
	parsed := importParsedValidatorFixture()
	parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: archived.ArtifactID, Path: "arrays/absolute_lut.npy", SHA: archived.SHA256, Contract: "AbsoluteLUTArray"})
	parsed.Files = append(parsed.Files, importParsedFile{Path: "arrays/absolute_lut.npy", Content: v2input.Ref{ArtifactID: staged, SHA256: archived.SHA256}, Contract: "AbsoluteLUTArray"})
	parsedRaw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	codec := &dependencyCodecStub{}
	remapper, err := NewRecursiveImportDependencyRemapper(uuid.New(), parsedRaw, remapArchiveStub{}, &importRecipePorts{old: old, target: target}, codec, codec)
	if err != nil {
		t.Fatal(err)
	}
	_, err = remapper.RemapImportRecipeDependency(context.Background(), parsed.Manifest.Entries[0].ID, archived, "AbsoluteLUTArray", old, target)
	var unsupported *ImportRecipeUnsupported
	if !errors.As(err, &unsupported) || unsupported.Reason != "binary_dependency_codec_not_installed" {
		t.Fatalf("LUT must require a physical binary codec: %v", err)
	}
	if codec.calls != 0 || len(codec.published) != 0 {
		t.Fatal("binary LUT entered JSON codec or was published")
	}
}

func TestRecursiveImportRemapperRoutesDefectLabelMapToBinaryCodec(t *testing.T) {
	archived := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("defect labels fixture"))}
	parsed := importParsedValidatorFixture()
	parsed.Manifest.Inventory = append(parsed.Manifest.Inventory, importBundleInventory{ID: archived.ArtifactID, Path: "arrays/defect_labels.npy", SHA: archived.SHA256, Contract: "DefectLabelMap"})
	parsed.Files = append(parsed.Files, importParsedFile{Path: "arrays/defect_labels.npy", Content: v2input.Ref{ArtifactID: uuid.New(), SHA256: archived.SHA256}, Contract: "DefectLabelMap"})
	parsedRaw, err := canonical.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	codec := &dependencyCodecStub{}
	remapper, err := NewRecursiveImportDependencyRemapper(uuid.New(), parsedRaw, remapArchiveStub{}, &importRecipePorts{old: old, target: target}, codec, codec)
	if err != nil {
		t.Fatal(err)
	}
	_, err = remapper.RemapImportRecipeDependency(context.Background(), parsed.Manifest.Entries[0].ID, archived, "DefectLabelMap", old, target)
	var unsupported *ImportRecipeUnsupported
	if !errors.As(err, &unsupported) || unsupported.Reason != "binary_dependency_codec_not_installed" {
		t.Fatalf("label map must require a physical binary codec: %v", err)
	}
	if codec.calls != 0 || len(codec.published) != 0 {
		t.Fatal("binary label map entered JSON codec or was published")
	}
}
