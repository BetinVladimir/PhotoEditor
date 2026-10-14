package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

// Metadata observation, never encoded byte acceptance or a download grant.
type MultiPreviewOutput struct {
	Task, Result, Artifact uuid.UUID
	Attempt                int
	Fence                  int64
	Params                 string
	Role, Bucket           string
	Object                 objectstore.Object
}
type MultiPreviewResults struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s MultiPreviewResults) Read(ctx context.Context, tenant, run uuid.UUID, variant string) ([]MultiPreviewOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || (variant != "preview1600" && variant != "preview2560") {
		return nil, objectstore.ErrInvalid
	}
	var outputs []MultiPreviewOutput
	err := withMultiPreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.v29_multi_preview_result($1,$2,$3)", tenant, run, variant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o MultiPreviewOutput
			if err := rows.Scan(&o.Task, &o.Result, &o.Attempt, &o.Fence, &o.Params, &o.Role, &o.Artifact, &o.Bucket, &o.Object.Key, &o.Object.VersionID, &o.Object.SHA256, &o.Object.Bytes, &o.Object.ContentType); err != nil {
				return err
			}
			outputs = append(outputs, o)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(outputs) == 0 {
		return nil, nil
	}
	if err := validateMultiPreviewOutputs(run, variant, s.Bucket, outputs); err != nil {
		return nil, err
	}
	return outputs, nil
}
func validateMultiPreviewOutputs(run uuid.UUID, variant, bucket string, outputs []MultiPreviewOutput) error {
	if len(outputs) != 2 {
		return objectstore.ErrIntegrity
	}
	first := outputs[0]
	seen := map[string]bool{}
	for _, o := range outputs {
		if o.Task != uuid.NewSHA1(run, []byte("render.derivative:"+variant)) || o.Result == uuid.Nil || o.Artifact == uuid.Nil || o.Artifact == o.Task || o.Attempt < 1 || o.Fence < 1 || o.Task != first.Task || o.Result != first.Result || o.Attempt != first.Attempt || o.Fence != first.Fence || o.Params != first.Params || len(o.Params) < 2 || len(o.Params) > 1048576 || o.Bucket != bucket || o.Object.Key == "" || o.Object.VersionID == "" || o.Object.VersionID == "null" || domainDigestInvalid(o.Object.SHA256) || o.Object.Bytes < 1 || seen[o.Role] {
			return objectstore.ErrIntegrity
		}
		seen[o.Role] = true
		switch o.Role {
		case "derivative":
			if (o.Object.ContentType != "image/jpeg" && o.Object.ContentType != "image/webp") || o.Object.Bytes > 54525952 {
				return objectstore.ErrIntegrity
			}
		case "derivative_manifest":
			if o.Object.ContentType != "application/json" || o.Object.Bytes > 1048576 {
				return objectstore.ErrIntegrity
			}
		default:
			return objectstore.ErrIntegrity
		}
	}
	if outputs[0].Artifact == outputs[1].Artifact {
		return objectstore.ErrIntegrity
	}
	return nil
}
