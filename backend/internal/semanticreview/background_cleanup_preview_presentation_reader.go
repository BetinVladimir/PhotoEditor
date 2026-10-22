package semanticreview

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

// ReadBackgroundCleanupPreviewPresentationDocument proves canonical producer identity and
// exact object bytes, with a second authority lookup after object IO. Display
// evidence and sidecars still require independent validation before exposing URLs.
func ReadBackgroundCleanupPreviewPresentationDocument(ctx context.Context, db artifactjson.RowQuerier,
	store planObjectStore, bucket string, who auth.Identity, run uuid.UUID,
	params BackgroundCleanupPreviewParams) (PreviewPresentationDocument, error) {
	empty := PreviewPresentationDocument{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || run == uuid.Nil {
		return empty, ErrInvalid
	}
	read, err := NewPlanObjectReader(store, bucket)
	if err != nil {
		return empty, err
	}
	body, digest, err := FreezeBackgroundCleanupPreviewParams(params)
	if err != nil {
		return empty, err
	}
	load := func(ctx context.Context) (previewEvidenceSource, error) {
		var s previewEvidenceSource
		err := db.QueryRow(ctx, "SELECT task_id,result_id,attempt,fence,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_background_cleanup_preview_presentation_source($1,$2,$3,$4,$5,$6)",
			params.TenantID, run, who.UserID, who.SessionID, string(body), digest).Scan(
			&s.TaskID, &s.ResultID, &s.Attempt, &s.Fence, &s.Object.Ref.ArtifactID, &s.Object.Ref.SHA256,
			&s.Object.Bucket, &s.Object.Key, &s.Object.Version, &s.Object.Bytes, &s.Object.MediaType)
		if errors.Is(err, pgx.ErrNoRows) {
			return s, ErrUnavailable
		}
		return s, err
	}
	expected := uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+params.TargetID.String()))
	return readBoundPreviewPresentationDocument(ctx, load, read, run, expected)
}
