package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

// SQLBackgroundCleanupPreviewResolver resolves only an existing current preview. Rendering
// is admitted separately; absence never silently creates an approved edit.
func SQLBackgroundCleanupPreviewResolver(db *pgxpool.Pool, store planObjectStore, bucket string) (BackgroundCleanupPreviewResolver, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	read, err := NewPlanObjectReader(store, bucket)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent, target uuid.UUID, request PreviewRequest) (BackgroundCleanupPreviewLookup, error) {
		empty := BackgroundCleanupPreviewLookup{}
		if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, target, request); err != nil {
			return empty, err
		}
		var run uuid.UUID
		var raw []byte
		var digest string
		var source PlanSource
		err := db.QueryRow(ctx, "SELECT preview_id,params,params_sha,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.lookup_semantic_background_cleanup_preview($1,$2,$3,$4,$5,$6,$7,$8,$9)",
			tenant, intent, target, who.UserID, who.SessionID, request.ExpectedRevision, request.TargetRevision, request.PlanSHA, request.CandidateSHA).Scan(
			&run, &raw, &digest, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bucket, &source.Key, &source.Version, &source.Bytes, &source.MediaType)
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, ErrUnavailable
		}
		if err != nil {
			return empty, err
		}
		var params BackgroundCleanupPreviewParams
		if len(raw) < 2 || len(raw) > 1048576 || decodeStrict(raw, &params) != nil {
			return empty, ErrInvalid
		}
		_, actual, err := FreezeBackgroundCleanupPreviewParams(params)
		if err != nil || actual != digest || run == uuid.Nil || params.TenantID != tenant || params.IntentID != intent || params.TargetID != target ||
			params.WorkflowRevision != request.ExpectedRevision || params.TargetRevision != request.TargetRevision ||
			params.Plan.SHA256 != request.PlanSHA || params.Candidate.SHA256 != request.CandidateSHA || source.Ref != params.Source.Source {
			return empty, ErrInvalid
		}
		bytes, err := read(ctx, source)
		if err != nil {
			return empty, err
		}
		if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, target, request); err != nil {
			return empty, err
		}
		return BackgroundCleanupPreviewLookup{RunID: run, Params: params, SourceRaw: append([]byte(nil), bytes...)}, nil
	}, nil
}
