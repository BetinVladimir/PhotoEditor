package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

// PreviewPresentationDocument binds bytes to a live canonical producer. It is
// not validated display evidence: sidecars, numerical replay linkage and PNGs
// must be verified before exposing URLs or constructing a PreviewDTO.
type PreviewPresentationDocument struct {
	Source           PlanSource
	TaskID, ResultID uuid.UUID
	Attempt          int
	Fence            int64
	raw              []byte
}

func ReadExposurePreviewPresentationDocument(ctx context.Context, db artifactjson.RowQuerier,
	store planObjectStore, bucket string, who auth.Identity, run uuid.UUID,
	params ExposurePreviewParams) (PreviewPresentationDocument, error) {
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
	body, digest, err := FreezeExposurePreviewParams(params)
	if err != nil {
		return empty, err
	}
	load := func(ctx context.Context) (previewEvidenceSource, error) {
		var s previewEvidenceSource
		err := db.QueryRow(ctx, "SELECT task_id,result_id,attempt,fence,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_preapply_preview_presentation_source($1,$2,$3,$4,$5,$6)",
			params.TenantID, run, who.UserID, who.SessionID, string(body), digest).Scan(
			&s.TaskID, &s.ResultID, &s.Attempt, &s.Fence, &s.Object.Ref.ArtifactID, &s.Object.Ref.SHA256,
			&s.Object.Bucket, &s.Object.Key, &s.Object.Version, &s.Object.Bytes, &s.Object.MediaType)
		if errors.Is(err, pgx.ErrNoRows) {
			return s, ErrUnavailable
		}
		return s, err
	}
	return readPreviewPresentationDocument(ctx, load, read, run, params)
}

func readPreviewPresentationDocument(ctx context.Context,
	load func(context.Context) (previewEvidenceSource, error), read PlanObjectReader,
	run uuid.UUID, params ExposurePreviewParams) (PreviewPresentationDocument, error) {
	expected := uuid.NewSHA1(run, []byte("semantic.preview_local_exposure:"+params.TargetID.String()))
	return readBoundPreviewPresentationDocument(ctx, load, read, run, expected)
}

func readBoundPreviewPresentationDocument(ctx context.Context,
	load func(context.Context) (previewEvidenceSource, error), read PlanObjectReader,
	run, expected uuid.UUID) (PreviewPresentationDocument, error) {
	empty := PreviewPresentationDocument{}
	if load == nil || read == nil || run == uuid.Nil || expected == uuid.Nil {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	source, err := load(ctx)
	if err != nil {
		return empty, err
	}
	if source.TaskID != expected || source.ResultID == uuid.Nil || source.Attempt < 1 || source.Fence < 1 ||
		source.Object.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(source.Object.Ref.SHA256) ||
		source.Object.MediaType != "application/json" || source.Object.Bytes < 2 || source.Object.Bytes > 1048576 {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	raw, err := read(ctx, source.Object)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != source.Object.Bytes || hex.EncodeToString(sum[:]) != source.Object.Ref.SHA256 {
		return empty, ErrInvalid
	}
	// Own verified bytes before reauthorization can reuse the storage buffer.
	raw = append([]byte(nil), raw...)
	current, err := load(ctx)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if current != source {
		return empty, ErrConflict
	}
	return PreviewPresentationDocument{Source: source.Object, TaskID: source.TaskID, ResultID: source.ResultID,
		Attempt: source.Attempt, Fence: source.Fence, raw: raw}, nil
}
