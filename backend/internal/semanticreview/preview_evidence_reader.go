package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
)

type previewEvidenceSource struct {
	TaskID, ResultID uuid.UUID
	Attempt          int
	Fence            int64
	Object           PlanSource
}

type VerifiedPreviewEvidence struct {
	TaskID, ResultID uuid.UUID
	Evidence         ExposurePreviewEvidence
	Source           PlanSource
	raw              []byte
}

// ReadExposurePreviewEvidence repeats canonical producer/session authority after
// bounded version-pinned object IO. It does not validate output pixels or grant Apply.
func ReadExposurePreviewEvidence(ctx context.Context, db artifactjson.RowQuerier, store planObjectStore,
	bucket string, who auth.Identity, run uuid.UUID, params ExposurePreviewParams) (VerifiedPreviewEvidence, error) {
	empty := VerifiedPreviewEvidence{}
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
	body, sha, err := FreezeExposurePreviewParams(params)
	if err != nil {
		return empty, err
	}
	load := func(ctx context.Context) (previewEvidenceSource, error) {
		var s previewEvidenceSource
		err := db.QueryRow(ctx, "SELECT task_id,result_id,attempt,fence,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_preapply_preview_evidence_source($1,$2,$3,$4,$5,$6)",
			params.TenantID, run, who.UserID, who.SessionID, string(body), sha).Scan(&s.TaskID, &s.ResultID, &s.Attempt, &s.Fence,
			&s.Object.Ref.ArtifactID, &s.Object.Ref.SHA256, &s.Object.Bucket, &s.Object.Key, &s.Object.Version, &s.Object.Bytes, &s.Object.MediaType)
		return s, err
	}
	return readExposurePreviewEvidence(ctx, load, read, run, params)
}

func readExposurePreviewEvidence(ctx context.Context, load func(context.Context) (previewEvidenceSource, error),
	read PlanObjectReader, run uuid.UUID, params ExposurePreviewParams) (VerifiedPreviewEvidence, error) {
	empty := VerifiedPreviewEvidence{}
	if load == nil || read == nil {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	source, err := load(ctx)
	if err != nil {
		return empty, err
	}
	expected := uuid.NewSHA1(run, []byte("semantic.preview_local_exposure:"+params.TargetID.String()))
	if source.TaskID != expected || source.ResultID == uuid.Nil || source.Attempt < 1 || source.Fence < 1 ||
		source.Object.Bytes < 2 || source.Object.Bytes > 1048576 {
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
	if int64(len(raw)) != source.Object.Bytes {
		return empty, ErrInvalid
	}
	evidence, err := DecodeExposurePreviewEvidence(source.Object.Ref, raw, params)
	if err != nil {
		return empty, err
	}
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
	return VerifiedPreviewEvidence{TaskID: source.TaskID, ResultID: source.ResultID, Evidence: evidence, Source: source.Object, raw: append([]byte(nil), raw...)}, nil
}
