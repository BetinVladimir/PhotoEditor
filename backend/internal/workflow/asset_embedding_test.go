package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type embeddingRepo struct {
	lease      AssetEmbeddingLease
	lock, code string
	values     []float64
}

func (r *embeddingRepo) Claim(context.Context) (*AssetEmbeddingLease, error) {
	v := r.lease
	return &v, nil
}
func (r *embeddingRepo) Finish(_ context.Context, _ AssetEmbeddingLease, lock string, values []float64, code string) (bool, error) {
	r.lock, r.values, r.code = lock, values, code
	return true, nil
}

type embeddingObjects struct{ raw []byte }

func (o embeddingObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o.raw)), nil
}
func embeddingBody(source uuid.UUID, sha string) []byte {
	values := make([]float64, 384)
	values[0] = 1
	raw, _ := json.Marshal(map[string]any{"image": map[string]any{"artifact_id": source, "sha256": sha}, "values": values, "model_id": "dinov2_vits14", "model_lock_id": string(bytes.Repeat([]byte{'b'}, 64)), "backend": "cpu", "preprocessing_id": "dinov2_pad518_v1", "source_clip_fraction": 0})
	return raw
}
func TestAssetEmbeddingMaterializerBindsSourceAndStoresVector(t *testing.T) {
	source := uuid.New()
	raw := embeddingBody(source, string(bytes.Repeat([]byte{'a'}, 64)))
	sum := sha256.Sum256(raw)
	repo := &embeddingRepo{lease: AssetEmbeddingLease{ID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), VersionID: uuid.New(), SourceID: source, SourceSHA: string(bytes.Repeat([]byte{'a'}, 64)), Object: objectstore.Object{SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(raw)), ContentType: "application/json"}}}
	done, err := (AssetEmbeddingMaterializer{Repository: repo, Objects: embeddingObjects{raw}}).ProcessOne(context.Background())
	if err != nil || !done || repo.code != "" || len(repo.values) != 384 {
		t.Fatalf("done=%v err=%v code=%s", done, err, repo.code)
	}
}
func TestAssetEmbeddingMaterializerRejectsWrongSource(t *testing.T) {
	source := uuid.New()
	raw := embeddingBody(uuid.New(), string(bytes.Repeat([]byte{'a'}, 64)))
	sum := sha256.Sum256(raw)
	repo := &embeddingRepo{lease: AssetEmbeddingLease{ID: uuid.New(), Token: uuid.New(), SourceID: source, SourceSHA: string(bytes.Repeat([]byte{'a'}, 64)), Object: objectstore.Object{SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(raw)), ContentType: "application/json"}}}
	done, _ := (AssetEmbeddingMaterializer{Repository: repo, Objects: embeddingObjects{raw}}).ProcessOne(context.Background())
	if !done || repo.code != "EMBEDDING_INVALID" {
		t.Fatalf("done=%v code=%s", done, repo.code)
	}
}
