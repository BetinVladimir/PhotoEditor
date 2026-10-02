package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type LensSelectionQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type SQLLensSelectionSource struct {
	DB      LensSelectionQuerier
	Objects SessionAnalysisObjectReader
	Bucket  string
}

// LensSelectedSource contains verified immutable document bytes; callers choose
// a qualified lens profile separately. No documents, quota or tasks are written.
type LensSelectedSource struct {
	Ordinal       int
	Binding       SessionVersionBinding
	SourceJSON    []byte
	Original      LensOriginalBinding
	OriginalBytes int64
}

type lensSelectedRow struct {
	ordinal        int
	version        uuid.UUID
	source         Ref
	sourceObject   objectstore.Object
	sourceBucket   string
	original       LensOriginalSource
	originalBucket string
	count          int64
}

// Load rejects the whole selection before object IO if catalog rows are missing,
// substituted or unordered. It then verifies each pinned WorkingImage's content
// and original hash. Admission must repeat authority under its transaction locks.
func (s SQLLensSelectionSource) Load(ctx context.Context, tenant, project, snapshot, actor, session uuid.UUID) ([]LensSelectedSource, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT ordinal,asset_version_id,source_id,source_sha,source_bytes,source_bucket,source_key,source_version,source_media_type,original_id,original_sha,original_bytes,original_bucket,original_key,original_version,original_media_type,item_count FROM photo.v25_lens_selection_sources($1,$2,$3,$4,$5)`, tenant, project, snapshot, actor, session)
	if err != nil {
		return nil, err
	}
	selected := []lensSelectedRow{}
	seen := map[uuid.UUID]bool{}
	var count int64
	for rows.Next() {
		var r lensSelectedRow
		err = rows.Scan(&r.ordinal, &r.version, &r.source.ArtifactID, &r.source.SHA256, &r.sourceObject.Bytes, &r.sourceBucket, &r.sourceObject.Key, &r.sourceObject.VersionID, &r.sourceObject.ContentType, &r.original.Ref.ArtifactID, &r.original.Ref.SHA256, &r.original.Object.Bytes, &r.originalBucket, &r.original.Object.Key, &r.original.Object.VersionID, &r.original.Object.ContentType, &r.count)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if len(selected) >= 5000 || r.ordinal != len(selected) || r.version == uuid.Nil || seen[r.version] || !validRef(r.source) || !validRef(r.original.Ref) || r.count < 1 || r.count > 5000 || (count != 0 && count != r.count) || r.sourceBucket != s.Bucket || r.originalBucket != s.Bucket || r.sourceObject.Key == "" || r.original.Object.Key == "" || r.sourceObject.VersionID == "" || r.sourceObject.VersionID == "null" || r.original.Object.VersionID == "" || r.original.Object.VersionID == "null" || r.sourceObject.ContentType != "application/json" || r.sourceObject.Bytes < 2 || r.sourceObject.Bytes > 1048576 || r.original.Object.Bytes < 1 {
			rows.Close()
			return nil, ErrInvalid
		}
		count = r.count
		seen[r.version] = true
		r.sourceObject.SHA256 = r.source.SHA256
		r.original.Object.SHA256 = r.original.Ref.SHA256
		selected = append(selected, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, ErrExpired
	}
	if int64(len(selected)) != count {
		return nil, ErrInvalid
	}
	result := make([]LensSelectedSource, 0, len(selected))
	for _, r := range selected {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		reader, err := s.Objects.Open(ctx, r.sourceObject)
		if err != nil {
			return nil, err
		}
		raw, readErr := io.ReadAll(io.LimitReader(reader, 1048577))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		digest := sha256.Sum256(raw)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if int64(len(raw)) != r.sourceObject.Bytes || hex.EncodeToString(digest[:]) != r.source.SHA256 {
			return nil, ErrInvalid
		}
		image, err := review.ParseWorkingImage(raw)
		if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Canvas.SourceSHA256 != r.original.Ref.SHA256 {
			return nil, ErrInvalid
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil {
			return nil, err
		}
		binding := SessionVersionBinding{r.version, r.source, canvas}
		original, err := CompileLensOriginalBinding(binding, r.original)
		if err != nil {
			return nil, err
		}
		result = append(result, LensSelectedSource{r.ordinal, binding, raw, original, r.original.Object.Bytes})
	}
	return result, nil
}
