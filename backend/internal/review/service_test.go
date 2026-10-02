package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
)

type serviceObjects struct {
	raw string
	err error
}

func (o serviceObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	if o.err != nil {
		return nil, o.err
	}
	return io.NopCloser(strings.NewReader(o.raw)), nil
}

func testObject(raw string) objectstore.Object {
	sum := sha256.Sum256([]byte(raw))
	return objectstore.Object{Key: "review/document.json", VersionID: "v1", SHA256: hex.EncodeToString(sum[:]), ContentType: "application/json", Bytes: int64(len(raw))}
}

func TestReadObjectVerifiesImmutableIdentity(t *testing.T) {
	raw := `{"version":"fixture"}`
	object := testObject(raw)
	got, err := readObject(context.Background(), serviceObjects{raw: raw}, object)
	if err != nil || string(got) != raw {
		t.Fatalf("read: %q %v", got, err)
	}
	badHash := object
	badHash.SHA256 = strings.Repeat("0", 64)
	if _, err = readObject(context.Background(), serviceObjects{raw: raw}, badHash); domain.AsError(err).Code != "ARTIFACT_HASH_MISMATCH" {
		t.Fatalf("hash mismatch: %v", err)
	}
	badSize := object
	badSize.Bytes++
	if _, err = readObject(context.Background(), serviceObjects{raw: raw}, badSize); domain.AsError(err).Code != "ARTIFACT_HASH_MISMATCH" {
		t.Fatalf("size mismatch: %v", err)
	}
}

func TestArtifactObjectRejectsUnsafeMetadata(t *testing.T) {
	sha := strings.Repeat("a", 64)
	if _, _, err := artifactObject(uuid.New(), "photo", "key", "null", sha, "application/json", 10); domain.AsError(err).Code != "ARTIFACT_MISSING" {
		t.Fatalf("null version: %v", err)
	}
	if _, _, err := artifactObject(uuid.New(), "photo", "key", "v1", sha, "image/jpeg", 10); domain.AsError(err).Code != "ARTIFACT_MISSING" {
		t.Fatalf("media: %v", err)
	}
	if _, _, err := artifactObject(uuid.New(), "photo", "key", "v1", sha, "application/json", 1048577); domain.AsError(err).Code != "ARTIFACT_MISSING" {
		t.Fatalf("size: %v", err)
	}
}
