package workflow

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

// RestorationPreviewResults observes the exact two output locators for one
// of the four sealed tasks. It does not accept encoded bytes or write a receipt.
type RestorationPreviewResults struct {
	DB     *pgxpool.Pool
	Bucket string
}

func (s RestorationPreviewResults) Read(ctx context.Context, tenant, run uuid.UUID, side, variant string) ([]MultiPreviewOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 ||
		(side != "before" && side != "after") || (variant != "preview1600" && variant != "preview2560") {
		return nil, objectstore.ErrInvalid
	}
	var outputs []MultiPreviewOutput
	err := withMultiPreviewDocumentTx(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.v28_restoration_preview_result($1,$2,$3,$4)", tenant, run, side, variant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o MultiPreviewOutput
			if err := rows.Scan(&o.Task, &o.Result, &o.Attempt, &o.Fence, &o.Params, &o.Role,
				&o.Artifact, &o.Bucket, &o.Object.Key, &o.Object.VersionID,
				&o.Object.SHA256, &o.Object.Bytes, &o.Object.ContentType); err != nil {
				return err
			}
			outputs = append(outputs, o)
		}
		return rows.Err()
	})
	if err != nil || len(outputs) == 0 {
		return outputs, err
	}
	if err := validateRestorationPreviewOutputs(run, side, variant, s.Bucket, outputs); err != nil {
		return nil, err
	}
	return outputs, nil
}

func validateRestorationPreviewOutputs(run uuid.UUID, side, variant, bucket string, outputs []MultiPreviewOutput) error {
	if run == uuid.Nil || (side != "before" && side != "after") ||
		(variant != "preview1600" && variant != "preview2560") || len(outputs) != 2 {
		return objectstore.ErrIntegrity
	}
	expected := restorationPreviewTaskID(run, side, variant)
	first := outputs[0]
	seen := map[string]bool{}
	for _, o := range outputs {
		if o.Task != expected || o.Result == uuid.Nil || o.Artifact == uuid.Nil ||
			o.Artifact == o.Task || o.Attempt < 1 || o.Fence < 1 ||
			o.Task != first.Task || o.Result != first.Result || o.Attempt != first.Attempt ||
			o.Fence != first.Fence || o.Params != first.Params || len(o.Params) < 2 ||
			len(o.Params) > 1048576 || o.Bucket != bucket || o.Object.Key == "" ||
			o.Object.VersionID == "" || o.Object.VersionID == "null" ||
			domainDigestInvalid(o.Object.SHA256) || o.Object.Bytes < 1 || seen[o.Role] {
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
