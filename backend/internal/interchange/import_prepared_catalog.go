package interchange

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// Each catalog instance is bound to one adopted entry and its current source.
// A ready result authorizes immutable reads, never processing/model approval.
type SQLPreparedImportCatalog struct {
	Store           SQLPreparedImportArtifactStore
	Scope           ImportPreparedScope
	BackgroundMedia string
}

func scanPreparedImportObject(row pgx.Row, scope ImportPreparedScope, ref v2input.Ref, bucket, media string, maxBytes int64) (objectstore.Object, error) {
	var object objectstore.Object
	var actualBucket string
	if err := row.Scan(&actualBucket, &object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType); err != nil {
		return objectstore.Object{}, err
	}
	key, err := objectstore.DocumentKey(scope.Tenant, scope.Run, ref.ArtifactID)
	if err != nil {
		return objectstore.Object{}, err
	}
	minimum := int64(1)
	if media == "application/json" {
		minimum = 2
	}
	if scope.Contract == "StyleBackgroundAsset" {
		minimum = 8
	}
	if actualBucket != bucket || object.Key != key || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.SHA256 != ref.SHA256 || object.ContentType != media || object.Bytes < minimum || object.Bytes > maxBytes {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	return object, nil
}

func (s SQLPreparedImportCatalog) LookupImportDocument(ctx context.Context, entry v2input.Entry) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	scope := s.Scope
	scope.Contract = entry.Contract
	media := "application/json"
	maxBytes := int64(8 << 20)
	probe := int64(2)
	if entry.Contract == "MaskPixels" || entry.Contract == "DenseWarpField" || entry.Contract == "AbsoluteLUTArray" || entry.Contract == "DefectLabelMap" || entry.Contract == "WorkingPixels" || entry.Contract == "WorkingAlpha" {
		media = "application/octet-stream"
		maxBytes = 128 << 30
		probe = 1
	}
 if entry.Contract=="DefectLabelPNG"{media="image/png";maxBytes=512<<20;probe=8}
	if entry.Contract == "StyleBackgroundAsset" {
		media = s.BackgroundMedia
		maxBytes = 536870912
		probe = 8
		switch media {
		case "image/jpeg", "image/png", "image/tiff":
		default:
			return objectstore.Object{}, v2input.ErrInvalid
		}
	}
	_, parsed, err := s.Store.receipt(scope, entry.Ref, probe, media)
	if err != nil {
		return objectstore.Object{}, err
	}
	binding, err := canonical.Marshal(scope.Binding)
	if err != nil {
		return objectstore.Object{}, err
	}
	var object objectstore.Object
	err = database.TenantTransaction(ctx, s.Store.DB, scope.Tenant, func(tx pgx.Tx) error {
		if err := s.Store.checkPreparedRun(ctx, tx); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_interchange_prepared_document($1,$2,$3,$4,$5::jsonb,$6,$7,$8)`, scope.Tenant, scope.Workflow, string(parsed), scope.Entry, string(binding), entry.Ref.ArtifactID, entry.Ref.SHA256, entry.Contract)
		var err error
		object, err = scanPreparedImportObject(row, scope, entry.Ref, s.Store.Bucket, media, maxBytes)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, artifactjson.ErrStale
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	return object, nil
}

func (s SQLPreparedImportArtifactStore) checkPreparedRun(ctx context.Context, tx pgx.Tx) error {
	return (SQLImportDocumentStore{Tenant: s.Tenant}).checkRun(ctx, tx, s.Identity)
}
