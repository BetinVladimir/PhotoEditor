package workflow

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// The pool releases each metadata query before version-pinned object IO.
// This read is an observation; publication/materialization must recheck authority.
type RenderDerivativeLoader struct {
	DB      *pgxpool.Pool
	Objects ArtifactOpener
	Bucket  string
}

func (l RenderDerivativeLoader) json(ctx context.Context, intent v2render.RenderDerivativeIntent, ref v2input.Ref) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l.DB == nil || l.Objects == nil || l.Bucket == "" || intent.Tenant == uuid.Nil || intent.Run == uuid.Nil || ref.ArtifactID == uuid.Nil {
		return nil, objectstore.ErrInvalid
	}
	var id uuid.UUID
	var bucket string
	var o objectstore.Object
	err := l.DB.QueryRow(ctx, "SELECT * FROM photo.v26_render_derivative_artifact($1,$2,$3,$4)", intent.Tenant, intent.Run, ref.ArtifactID, ref.SHA256).Scan(&id, &bucket, &o.Key, &o.VersionID, &o.SHA256, &o.Bytes, &o.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, objectstore.ErrMissing
	}
	if err != nil {
		return nil, err
	}
	if id != ref.ArtifactID || bucket != l.Bucket || o.SHA256 != ref.SHA256 || o.Bytes < 2 || o.Bytes > 1048576 || o.ContentType != "application/json" || o.VersionID == "" || o.VersionID == "null" || o.Key == "" {
		return nil, objectstore.ErrIntegrity
	}
	return readRenderDerivativeJSON(ctx, l.Objects, o)
}

func readRenderDerivativeJSON(ctx context.Context, objects ArtifactOpener, o objectstore.Object) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if objects == nil || o.Bytes < 2 || o.Bytes > 1048576 || o.ContentType != "application/json" || o.VersionID == "" || o.VersionID == "null" || o.Key == "" {
		return nil, objectstore.ErrIntegrity
	}
	r, err := objects.Open(ctx, o)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(r, o.Bytes+1))
	closeErr := r.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(body)) != o.Bytes || renderDerivativeBytesSHA(body) != o.SHA256 {
		return nil, objectstore.ErrIntegrity
	}
	return body, nil
}

func (l RenderDerivativeLoader) Load(ctx context.Context, intent v2render.RenderDerivativeIntent) (root, image []byte, err error) {
	root, err = l.json(ctx, intent, intent.Root)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := v2draft.ParseImageResult(root, intent.Root)
	if err != nil {
		return nil, nil, err
	}
	image, err = l.json(ctx, intent, parsed.WorkingImage)
	if err != nil {
		return nil, nil, err
	}
	if _, err = BuildRenderDerivativeDocuments(intent, root, image); err != nil {
		return nil, nil, err
	}
	return root, image, nil
}
