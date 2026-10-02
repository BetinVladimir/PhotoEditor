package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

type AssetEmbeddingLease struct {
	ID, Token, TenantID, VersionID, SourceID, ArtifactID uuid.UUID
	SourceSHA                                            string
	Object                                               objectstore.Object
}
type AssetEmbeddingRepository interface {
	Claim(context.Context) (*AssetEmbeddingLease, error)
	Finish(context.Context, AssetEmbeddingLease, string, []float64, string) (bool, error)
}
type SQLAssetEmbeddingRepository struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	Bucket string
}

func (r SQLAssetEmbeddingRepository) Claim(ctx context.Context) (*AssetEmbeddingLease, error) {
	v := new(AssetEmbeddingLease)
	var bucket, media string
	e := r.DB.QueryRow(ctx, `SELECT id,token,tenant_id,asset_version_id,source_artifact_id,source_sha256,artifact_id,bucket,object_key,object_version,sha256,bytes,media_type FROM photo.claim_asset_embedding()`).Scan(&v.ID, &v.Token, &v.TenantID, &v.VersionID, &v.SourceID, &v.SourceSHA, &v.ArtifactID, &bucket, &v.Object.Key, &v.Object.VersionID, &v.Object.SHA256, &v.Object.Bytes, &media)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v.Object.ContentType = media
	if bucket != r.Bucket || media != "application/json" || v.Object.Bytes < 2 || v.Object.Bytes > 1048576 {
		return nil, objectstore.ErrIntegrity
	}
	return v, nil
}
func (r SQLAssetEmbeddingRepository) Finish(ctx context.Context, v AssetEmbeddingLease, lock string, values []float64, code string) (bool, error) {
	var ok bool
	var data any = values
	if code != "" {
		lock = ""
		data = nil
	}
	e := r.DB.QueryRow(ctx, `SELECT photo.finish_asset_embedding($1,$2,$3,$4,$5)`, v.ID, v.Token, lock, data, nullableString(code)).Scan(&ok)
	return ok, e
}

type AssetEmbeddingMaterializer struct {
	Repository AssetEmbeddingRepository
	Objects    ArtifactOpener
}

func (m AssetEmbeddingMaterializer) ProcessOne(ctx context.Context) (bool, error) {
	l, e := m.Repository.Claim(ctx)
	if e != nil || l == nil {
		return false, e
	}
	fail := func(code string, cause error) (bool, error) {
		ok, x := m.Repository.Finish(ctx, *l, "", nil, code)
		if x != nil {
			return false, x
		}
		if !ok {
			return false, errors.New("embedding lease lost")
		}
		return true, cause
	}
	rd, e := m.Objects.Open(ctx, l.Object)
	if e != nil {
		return fail("EMBEDDING_READ_FAILED", e)
	}
	defer rd.Close()
	raw, e := io.ReadAll(io.LimitReader(rd, 1048577))
	if e != nil {
		return fail("EMBEDDING_READ_FAILED", e)
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != l.Object.Bytes || hex.EncodeToString(sum[:]) != l.Object.SHA256 {
		return fail("EMBEDDING_INTEGRITY_FAILED", objectstore.ErrIntegrity)
	}
	var v struct {
		Image struct {
			ArtifactID uuid.UUID `json:"artifact_id"`
			SHA        string    `json:"sha256"`
		} `json:"image"`
		Values     []float64 `json:"values"`
		Model      string    `json:"model_id"`
		Lock       string    `json:"model_lock_id"`
		Preprocess string    `json:"preprocessing_id"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Image.ArtifactID != l.SourceID || v.Image.SHA != l.SourceSHA || v.Model != "dinov2_vits14" || v.Preprocess != "dinov2_pad518_v1" || len(v.Values) != 384 {
		return fail("EMBEDDING_INVALID", errors.New("invalid embedding contract"))
	}
	norm := 0.0
	for _, x := range v.Values {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fail("EMBEDDING_INVALID", errors.New("nonfinite embedding"))
		}
		norm += x * x
	}
	if math.Sqrt(norm) < .999 || math.Sqrt(norm) > 1.001 {
		return fail("EMBEDDING_INVALID", errors.New("embedding not normalized"))
	}
	ok, e := m.Repository.Finish(ctx, *l, v.Lock, v.Values, "")
	if e != nil {
		return false, e
	}
	if !ok {
		return false, errors.New("embedding lease lost")
	}
	return true, nil
}
