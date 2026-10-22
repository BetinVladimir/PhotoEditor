package semanticreview

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// ReadRetouchPreviewPresentationDocument proves canonical producer identity and
// exact object bytes, with a second authority lookup after object IO. Display
// evidence and sidecars still require independent validation before exposing URLs.
func ReadRetouchPreviewPresentationDocument(ctx context.Context, db artifactjson.RowQuerier,
	store planObjectStore, bucket string, who auth.Identity, run uuid.UUID,
	params RetouchPreviewParams, adapterContext v2input.Ref) (PreviewPresentationDocument, error) {
	empty := PreviewPresentationDocument{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if adapterContext.ArtifactID == uuid.Nil || !shaPattern.MatchString(adapterContext.SHA256) || db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || run == uuid.Nil {
		return empty, ErrInvalid
	}
	read, err := NewPlanObjectReader(store, bucket)
	if err != nil {
		return empty, err
	}
	body, digest, err := FreezeRetouchPreviewParams(params)
	if err != nil {
		return empty, err
	}
	load := func(ctx context.Context) (previewEvidenceSource, error) {
		var s previewEvidenceSource
		err := db.QueryRow(ctx, "SELECT task_id,result_id,attempt,fence,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_retouch_preview_presentation_source($1,$2,$3,$4,$5,$6,$7,$8)",
			params.TenantID, run, who.UserID, who.SessionID, string(body), digest, adapterContext.ArtifactID, adapterContext.SHA256).Scan(
			&s.TaskID, &s.ResultID, &s.Attempt, &s.Fence, &s.Object.Ref.ArtifactID, &s.Object.Ref.SHA256,
			&s.Object.Bucket, &s.Object.Key, &s.Object.Version, &s.Object.Bytes, &s.Object.MediaType)
		if errors.Is(err, pgx.ErrNoRows) {
			return s, ErrUnavailable
		}
		return s, err
	}
	expected := uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+params.TargetID.String()))
	return readBoundPreviewPresentationDocument(ctx, load, read, run, expected)
}
