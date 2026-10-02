package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
)

type IngestUploadBinding struct{ AssetID, VersionID, ItemID uuid.UUID }
type IngestUploadBinder struct{ DB artifactjson.RowQuerier }

// Bind is an internal creation-time operation. The caller must authorize the
// upload/ingest request before creating the run; this is not a public ACL API.
func (b IngestUploadBinder) Bind(ctx context.Context, tenant, run uuid.UUID, ordinal int64, upload uuid.UUID) (IngestUploadBinding, error) {
	if b.DB == nil || tenant == uuid.Nil || run == uuid.Nil || upload == uuid.Nil || ordinal < 0 || ordinal > 9007199254740991 {
		return IngestUploadBinding{}, errors.New("invalid ingest upload binding")
	}
	var result IngestUploadBinding
	err := b.DB.QueryRow(ctx, "SELECT asset_id,version_id,item_id FROM photo.bind_ingest_upload($1,$2,$3,$4)", tenant, run, ordinal, upload).Scan(&result.AssetID, &result.VersionID, &result.ItemID)
	if err != nil {
		return IngestUploadBinding{}, err
	}
	if result.AssetID == uuid.Nil || result.VersionID == uuid.Nil || result.ItemID == uuid.Nil || result.AssetID == result.VersionID || result.AssetID == result.ItemID || result.VersionID == result.ItemID {
		return IngestUploadBinding{}, errors.New("invalid catalog identities")
	}
	return result, nil
}
