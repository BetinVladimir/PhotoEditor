package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

func TestReadCaptureShotListRejectsSwappedAndNonCanonicalObjects(t *testing.T) {
	raw := []byte(`{"requirements":[],"schema_version":1}`)
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	ref := Ref{uuid.New(), sha}
	obj := objectstore.Object{SHA256: sha, Bytes: int64(len(raw)), VersionID: "pinned-v1", ContentType: "application/json"}
	shots, err := ReadCaptureShotList(context.Background(), sessionImageBytes(raw), obj, ref)
	if err != nil || shots.Requirements == nil || len(shots.Requirements) != 0 {
		t.Fatalf("shots=%v error=%v", shots, err)
	}
	if _, err = ReadCaptureShotList(context.Background(), sessionImageBytes([]byte(`{"requirements":null,"schema_version":1}`)), obj, ref); err == nil {
		t.Fatal("swapped bytes accepted")
	}
	obj.VersionID = "null"
	if _, err = ReadCaptureShotList(context.Background(), sessionImageBytes(raw), obj, ref); err == nil {
		t.Fatal("unversioned object accepted")
	}
	raw = []byte(`{"schema_version":1, "requirements":[]}`)
	sum = sha256.Sum256(raw)
	sha = hex.EncodeToString(sum[:])
	ref.SHA256 = sha
	obj = objectstore.Object{SHA256: sha, Bytes: int64(len(raw)), VersionID: "pinned-v2", ContentType: "application/json"}
	if _, err = ReadCaptureShotList(context.Background(), sessionImageBytes(raw), obj, ref); err == nil {
		t.Fatal("noncanonical body accepted")
	}
}
