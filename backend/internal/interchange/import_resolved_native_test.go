package interchange

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type importResolvedScopeStub struct {
	archived, target int
	deny             bool
}

func (s *importResolvedScopeStub) QualifyArchivedResolvedScope(_ context.Context, _ []byte, _ v2input.SessionVersionBinding, children []v2input.Entry) error {
	s.archived++
	if s.deny || len(children) < 1 {
		return v2input.ErrInvalid
	}
	return nil
}
func (s *importResolvedScopeStub) QualifyTargetResolvedScope(_ context.Context, _ []byte, _ v2input.SessionVersionBinding, children []v2input.Entry) error {
	s.target++
	if s.deny || len(children) < 1 {
		return v2input.ErrInvalid
	}
	return nil
}

func TestNativeImportResolvedQualifierRequiresProbeAndSourceScope(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/resolved-binding-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(fixture, &body); err != nil {
		t.Fatal(err)
	}
	params := body["params"].(map[string]any)
	// A source image and style are distinct typed artifact roles.
	params["style"].(map[string]any)["artifact_id"] = uuid.NewString()
	var image v2input.Ref
	imageRaw, err := canonical.Marshal(params["image"])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(imageRaw, &image); err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{Source: image, AssetVersionID: uuid.New(), CanvasHash: rawSHA([]byte("canvas"))}
	delete(body, "hash")
	hash, err := canonical.Hash(body)
	if err != nil {
		t.Fatal(err)
	}
	body["hash"] = hash
	raw, err := canonical.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	scratch, err := NewExportArchiveScratch(root, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	runtime := rawSHA([]byte("runtime"))
	probe := &exportStyleProbeStub{runtime: runtime}
	scope := &importResolvedScopeStub{}
	qualifier, err := NewNativeImportResolvedQualifier(scratch, probe, map[string]string{}, runtime, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := qualifier.ValidateArchivedImportResolved(context.Background(), raw, binding); err != nil {
		t.Fatal(err)
	}
	if err := qualifier.QualifyImportedResolved(context.Background(), raw, binding); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 2 || scope.archived != 1 || scope.target != 1 {
		t.Fatal("native or scope qualification skipped")
	}
	scope.deny = true
	if err := qualifier.QualifyImportedResolved(context.Background(), raw, binding); err == nil {
		t.Fatal("scope denial ignored")
	}
	if err := qualifier.QualifyImportedResolved(context.Background(), raw, v2input.SessionVersionBinding{}); err == nil {
		t.Fatal("invalid binding accepted")
	}
	if probe.calls != 3 {
		t.Fatal("invalid binding reached native probe")
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 || scratch.used != 0 {
		t.Fatal("native scratch leaked", err)
	}
}
