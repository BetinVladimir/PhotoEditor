package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

// SQLSessionAnalysisSelectionSource compiles the pre-freeze InputSnapshot from
// authorized selection rows. A missing recipe or image rejects the whole
// selection; freeze repeats the relational preflight and copies those rows.
type SQLSessionAnalysisSelectionSource struct {
	DB      *pgxpool.Pool
	Objects SessionAnalysisObjectReader
	Bucket  string
}

type SessionAnalysisSelection struct {
	Tenant, Run, Session, Snapshot, Actor, AuthSession uuid.UUID
	ExpectedRevision                                   int64
	ManualReferences                                   json.RawMessage
}

func (s SQLSessionAnalysisSelectionSource) Load(ctx context.Context,
	request SessionAnalysisSelection) (SessionInputSnapshot, []byte, error) {
	var empty SessionInputSnapshot
	if s.DB == nil || s.Objects == nil || s.Bucket == "" ||
		request.Tenant == uuid.Nil || request.Run == uuid.Nil ||
		request.Session == uuid.Nil || request.Snapshot == uuid.Nil ||
		request.Actor == uuid.Nil || request.AuthSession == uuid.Nil ||
		request.ExpectedRevision < 1 || len(request.ManualReferences) < 2 {
		return empty, nil, ErrInvalid
	}
	rows, err := s.DB.Query(ctx, `SELECT ordinal,asset_version_id,artifact_id,sha256,
	 bytes,bucket,object_key,object_version,media_type,captured_at,camera_hash,
	 recipe_artifact_id,recipe_sha256,recipe_bytes,recipe_bucket,recipe_key,
	 recipe_version,recipe_media_type,selection_item_count
	 FROM photo.v22_analysis_selection_sources($1,$2,$3,$4,$5::jsonb,$6,$7)`,
		request.Tenant, request.Session, request.ExpectedRevision,
		request.Snapshot, string(request.ManualReferences),
		request.Actor, request.AuthSession)
	if err != nil {
		return empty, nil, err
	}
	type selected struct {
		Item         SessionSnapshotItem
		Object       objectstore.Object
		RecipeObject objectstore.Object
	}
	selectedRows := make([]selected, 0)
	var count int64
	for rows.Next() {
		var row selected
		var bucket string
		var recipeBucket string
		var itemCount int64
		item := &row.Item
		if err := rows.Scan(&item.Ordinal, &item.Binding.AssetVersionID,
			&item.WorkingImage.ArtifactID, &item.WorkingImage.SHA256,
			&row.Object.Bytes, &bucket, &row.Object.Key,
			&row.Object.VersionID, &row.Object.ContentType,
			&item.CapturedAt, &item.CameraHash,
			&item.Recipe.ArtifactID, &item.Recipe.SHA256,
			&row.RecipeObject.Bytes, &recipeBucket, &row.RecipeObject.Key,
			&row.RecipeObject.VersionID, &row.RecipeObject.ContentType,
			&itemCount); err != nil {
			rows.Close()
			return empty, nil, err
		}
		if len(selectedRows) >= 5000 || item.Ordinal != len(selectedRows) ||
			item.Binding.AssetVersionID == uuid.Nil ||
			!validRef(item.WorkingImage) || !validRef(item.Recipe) ||
			bucket != s.Bucket || recipeBucket != s.Bucket ||
			row.RecipeObject.VersionID == "" ||
			row.RecipeObject.VersionID == "null" || row.Object.VersionID == "" ||
			row.Object.VersionID == "null" || itemCount < 1 || itemCount > 5000 ||
			(count != 0 && count != itemCount) {
			rows.Close()
			return empty, nil, ErrInvalid
		}
		count = itemCount
		row.Object.SHA256 = item.WorkingImage.SHA256
		row.RecipeObject.SHA256 = item.Recipe.SHA256
		item.Binding.Source = item.WorkingImage
		selectedRows = append(selectedRows, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, nil, err
	}
	if count != int64(len(selectedRows)) || count == 0 {
		return empty, nil, ErrInvalid
	}
	items := make([]SessionSnapshotItem, len(selectedRows))
	for i, row := range selectedRows {
		image, err := readVerifiedSessionWorkingImage(ctx, s.Objects,
			row.Object, row.Item.WorkingImage)
		if err != nil {
			return empty, nil, err
		}
		canvasSHA, err := canonical.Hash(image.Canvas)
		if err != nil || !digestPattern.MatchString(canvasSHA) {
			return empty, nil, ErrInvalid
		}
		row.Item.Binding.CanvasHash = canvasSHA
		if err := readSessionRecipe(ctx, s.Objects, row.RecipeObject,
			row.Item.Recipe, row.Item.Binding); err != nil {
			return empty, nil, err
		}
		items[i] = row.Item
	}
	return CompileSessionInputSnapshot(request.Run, items)
}
