package workflow

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type RestorationPreviewObjectCatalog interface {
	Resolve(context.Context, RestorationPreviewSource, string) (objectstore.Object, error)
}

type SQLRestorationPreviewObjects struct {
	DB     *pgxpool.Pool
	Bucket string
}

func restorationPreviewSideRef(pin RestorationPreviewSource, side string) (v2input.Ref, error) {
	if validateRestorationPreviewSource(pin) != nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	switch side {
	case "before":
		return pin.Base.Source, nil
	case "after":
		return pin.Candidate, nil
	default:
		return v2input.Ref{}, v2input.ErrInvalid
	}
}

func (s SQLRestorationPreviewObjects) Resolve(ctx context.Context, pin RestorationPreviewSource, side string) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	ref, err := restorationPreviewSideRef(pin, side)
	if err != nil || s.DB == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	var bucket string
	var obj objectstore.Object
	err = withPerspectivePreviewDocumentTx(ctx, s.DB, pin.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT * FROM photo.v28_restoration_preview_object($1,$2,$3,$4,$5,$6,$7,$8,$9)",
			pin.Tenant, pin.Producer, pin.Review, pin.ResultID, pin.Result.ArtifactID,
			pin.Result.SHA256, side, ref.ArtifactID, ref.SHA256).
			Scan(&bucket, &obj.Key, &obj.VersionID, &obj.Bytes, &obj.ContentType)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	obj.SHA256 = ref.SHA256
	if bucket != s.Bucket || obj.Key == "" || len(obj.Key) > 1024 ||
		obj.VersionID == "" || obj.VersionID == "null" || len(obj.VersionID) > 1024 ||
		obj.Bytes < 2 || obj.Bytes > 1048576 || obj.ContentType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	return obj, nil
}

type RestorationPreviewImageObjects interface {
	Open(context.Context, objectstore.Object) (io.ReadCloser, error)
}

func readRestorationPreviewImage(ctx context.Context, catalog RestorationPreviewObjectCatalog,
	objects RestorationPreviewImageObjects, pin RestorationPreviewSource, side string) ([]byte, error) {
	ref, err := restorationPreviewSideRef(pin, side)
	if err != nil || catalog == nil || objects == nil {
		return nil, v2input.ErrInvalid
	}
	obj, err := catalog.Resolve(ctx, pin, side)
	if err != nil {
		return nil, err
	}
	if obj.Bytes < 2 || obj.Bytes > 1048576 || obj.ContentType != "application/json" ||
		obj.SHA256 != ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
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
	raw, readErr := io.ReadAll(io.LimitReader(reader, 1048577))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	canonicalRaw, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalRaw, raw) || int64(len(raw)) != obj.Bytes ||
		renderDerivativeBytesSHA(raw) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	current, err := catalog.Resolve(ctx, pin, side)
	if err != nil {
		return nil, err
	}
	if current != obj {
		return nil, v2input.ErrExpired
	}
	return bytes.Clone(raw), nil
}

// LoadRestorationPreviewImages returns only exact, current, immutable source
// and proposal JSON. Pixel-array provenance is separately required at seal.
func LoadRestorationPreviewImages(ctx context.Context, source SQLRestorationPreviewSource,
	catalog RestorationPreviewObjectCatalog, objects RestorationPreviewImageObjects,
	tenant, producer uuid.UUID) (RestorationPreviewSource, []byte, []byte, error) {
	var zero RestorationPreviewSource
	if err := ctx.Err(); err != nil {
		return zero, nil, nil, err
	}
	if tenant == uuid.Nil || producer == uuid.Nil || catalog == nil || objects == nil {
		return zero, nil, nil, v2input.ErrInvalid
	}
	pin, err := source.Resolve(ctx, tenant, producer)
	if err != nil {
		return zero, nil, nil, err
	}
	before, err := readRestorationPreviewImage(ctx, catalog, objects, pin, "before")
	if err != nil {
		return zero, nil, nil, err
	}
	after, err := readRestorationPreviewImage(ctx, catalog, objects, pin, "after")
	if err != nil {
		return zero, nil, nil, err
	}
	current, err := source.Resolve(ctx, tenant, producer)
	if err != nil {
		return zero, nil, nil, err
	}
	if current != pin {
		return zero, nil, nil, v2input.ErrExpired
	}
	if err := ctx.Err(); err != nil {
		return zero, nil, nil, err
	}
	return pin, before, after, nil
}
