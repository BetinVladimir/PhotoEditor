package v2input

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestCompileSessionInputSnapshotBindsWorkerDigest(t *testing.T) {
	run := uuid.New()
	source := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	items := []SessionSnapshotItem{{Ordinal: 0,
		Binding: SessionVersionBinding{AssetVersionID: uuid.New(), Source: source,
			CanvasHash: strings.Repeat("b", 64)}, WorkingImage: source,
		Recipe: Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}}}
	snapshot, body, err := CompileSessionInputSnapshot(run, items)
	if err != nil || snapshot.SnapshotID != uuid.NewSHA1(run, []byte("analysis.snapshot")) {
		t.Fatalf("invalid frozen snapshot: %+v %v", snapshot, err)
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatal(err)
	}
	delete(document, "snapshot_digest")
	digest, err := canonical.Hash(document)
	if err != nil || digest != snapshot.SnapshotDigest {
		t.Fatalf("worker digest mismatch: %s %s %v", digest, snapshot.SnapshotDigest, err)
	}
	items[0].Ordinal = 1
	if _, _, err := CompileSessionInputSnapshot(run, items); !errors.Is(err, ErrInvalid) {
		t.Fatalf("noncontiguous snapshot accepted: %v", err)
	}
	items[0].Ordinal = 0
	items[0].WorkingImage.ArtifactID = uuid.New()
	if _, _, err := CompileSessionInputSnapshot(run, items); !errors.Is(err, ErrInvalid) {
		t.Fatalf("source binding mismatch accepted: %v", err)
	}
}
