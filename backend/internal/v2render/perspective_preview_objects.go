package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"time"
)

type PerspectivePreviewObjectCatalog interface {
	ResolveObject(context.Context, PerspectivePreviewSource, v2draft.LayerRef) (objectstore.Object, error)
}
type SQLPerspectivePreviewObjects struct {
	DB     *pgxpool.Pool
	Bucket string
}

func perspectivePreviewNodeLimit(pin PerspectivePreviewSource, node v2draft.LayerRef) (string, int64, error) {
	if err := validatePerspectivePreviewSource(pin); err != nil {
		return "", 0, err
	}
	if !validLayerArtifactRef(node.Ref) {
		return "", 0, v2input.ErrInvalid
	}
	if node.Path == "$" {
		if node.Contract != "ImageResult" || node.Ref != pin.Result {
			return "", 0, v2input.ErrInvalid
		}
		return "application/json", 1048576, nil
	}
	if node.Path == "$/source" {
		if node.Contract != "WorkingImage" || node.Ref != pin.Base.Source {
			return "", 0, v2input.ErrInvalid
		}
		return "application/json", 65536, nil
	}
	if len(node.Path) > 512 || !adoptedChildPath.MatchString(node.Path) || node.Ref.ArtifactID == pin.Result.ArtifactID || node.Ref.ArtifactID == pin.Base.Source.ArtifactID {
		return "", 0, v2input.ErrInvalid
	}
	if node.Contract == "Artifact" && node.Path != "$/report/transform" {
		return "", 0, v2input.ErrInvalid
	}
	media, limit, ok := perspectiveChildObjectLimit(node.Contract)
	if !ok {
		return "", 0, v2input.ErrInvalid
	}
	return media, limit, nil
}

func (s SQLPerspectivePreviewObjects) ResolveObject(ctx context.Context, pin PerspectivePreviewSource, node v2draft.LayerRef) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	media, limit, err := perspectivePreviewNodeLimit(pin, node)
	if err != nil {
		return zero, err
	}
	if s.DB == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", pin.Tenant.String()); err != nil {
		return zero, err
	}
	var bucket string
	var obj objectstore.Object
	err = tx.QueryRow(ctx, "SELECT * FROM photo.v29_perspective_preview_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", pin.Tenant, pin.Producer, pin.ResultID, pin.Attempt, pin.Fence, pin.Result.ArtifactID, pin.Result.SHA256, node.Ref.ArtifactID, node.Ref.SHA256, node.Path, node.Contract).Scan(&bucket, &obj.Key, &obj.VersionID, &obj.Bytes, &obj.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	obj.SHA256 = node.Ref.SHA256
	if bucket != s.Bucket || obj.Key == "" || len(obj.Key) > 1024 || obj.VersionID == "" || obj.VersionID == "null" || len(obj.VersionID) > 1024 || obj.Bytes < 1 || obj.Bytes > limit || obj.ContentType != media {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return obj, nil
}

// ReadPerspectivePreviewDocument performs version-pinned IO outside SQL,
// verifies exact original canonical bytes, then repeats current authorization.
func ReadPerspectivePreviewDocument(ctx context.Context, catalog PerspectivePreviewObjectCatalog, objects v2input.SessionAnalysisObjectReader, pin PerspectivePreviewSource, node v2draft.LayerRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	media, limit, err := perspectivePreviewNodeLimit(pin, node)
	if err != nil {
		return nil, err
	}
	if media != "application/json" || catalog == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	obj, err := catalog.ResolveObject(ctx, pin, node)
	if err != nil {
		return nil, err
	}
	if obj.Bytes < 2 || obj.Bytes > limit || obj.ContentType != media || obj.SHA256 != node.Ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return nil, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	raw, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sum := sha256.Sum256(raw)
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) || hex.EncodeToString(sum[:]) != node.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	current, err := catalog.ResolveObject(ctx, pin, node)
	if err != nil {
		return nil, err
	}
	if current != obj {
		return nil, v2input.ErrExpired
	}
	return bytes.Clone(raw), nil
}
