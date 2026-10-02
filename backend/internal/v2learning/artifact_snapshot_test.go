package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestCompileTrainingArtifactSnapshot(t *testing.T) {
	run, project, profile := uuid.New(), uuid.New(), uuid.New()
	manifestSHA := strings.Repeat("a", 64)
	snapshot, body, bodySHA, err := CompileTrainingArtifactSnapshot(
		run, project, profile, manifestSHA)
	if err != nil {
		t.Fatal(err)
	}
	manifestID, err := TrainingDocumentID(run, trainingManifestPurpose)
	if err != nil || manifestID != snapshot.Items[0].Artifact.ArtifactID ||
		snapshot.SnapshotID != uuid.NewSHA1(run, []byte("training.snapshot")) ||
		snapshot.Items[0].OwnerID != profile ||
		snapshot.Items[0].Artifact.SHA256 != manifestSHA {
		t.Fatal("snapshot binding mismatch", err)
	}
	rootID, err := TrainingDocumentID(run, trainingSnapshotPurpose)
	if err != nil || rootID == manifestID {
		t.Fatal("document purposes must have distinct deterministic IDs")
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	delete(decoded, "snapshot_digest")
	without, err := canonical.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(without)
	if snapshot.SnapshotDigest != hex.EncodeToString(digest[:]) {
		t.Fatal("snapshot digest includes itself or differs from JCS")
	}
	fullDigest := sha256.Sum256(body)
	if bodySHA != hex.EncodeToString(fullDigest[:]) {
		t.Fatal("root body SHA mismatch")
	}
	if path := os.Getenv("PHOTO_TEST_SNAPSHOT_OUTPUT"); path != "" {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
