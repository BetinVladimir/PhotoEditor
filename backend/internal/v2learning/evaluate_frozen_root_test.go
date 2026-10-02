package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestReadFrozenRootDocumentRejectsChangedOrUnversionedBytes(t *testing.T) {
	body := []byte(`{"schema_version":1}`)
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	object := objectstore.Object{Key: "documents/recipe.json", VersionID: "v1",
		ContentType: "application/json", Bytes: int64(len(body)), SHA256: ref.SHA256}
	actual, err := ReadFrozenRootDocument(context.Background(),
		evaluationManifestBytes{body}, object, ref)
	if err != nil || string(actual) != string(body) {
		t.Fatalf("verified root rejected: %v", err)
	}
	if _, err := ReadFrozenRootDocument(context.Background(),
		evaluationManifestBytes{[]byte(`{"schema_version":2}`)}, object, ref); err == nil {
		t.Fatal("changed object bytes accepted")
	}
	object.VersionID = "null"
	if _, err := ReadFrozenRootDocument(context.Background(),
		evaluationManifestBytes{body}, object, ref); err == nil {
		t.Fatal("unversioned root accepted")
	}
}
