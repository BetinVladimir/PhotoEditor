package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// SQLRenderBatchFactory assembles recovery/preparation dependencies without
// granting activation or model qualification. Pools and storage clients remain
// owned by the service. Separate buckets permit read-only source storage and
// writable transport storage; each reference is checked by its own catalog.
type SQLRenderBatchFactory struct {
	QualificationPin             *RenderQualificationPin
	API, Scheduler               *pgxpool.Pool
	Sources                      v2input.SessionAnalysisObjectReader
	Documents                    artifactjson.Objects
	SourceBucket, DocumentBucket string
	SubmissionSHA                string
	SubmissionKey                string
}

func (f SQLRenderBatchFactory) ForProject(ctx context.Context, project uuid.UUID) (RenderBatchCoordinator, error) {
	if err := ctx.Err(); err != nil {
		return RenderBatchCoordinator{}, err
	}
	if f.API == nil || f.Scheduler == nil || f.API == f.Scheduler || f.Sources == nil || f.Documents == nil || project == uuid.Nil || len(f.SourceBucket) < 3 || len(f.SourceBucket) > 63 || len(f.DocumentBucket) < 3 || len(f.DocumentBucket) > 63 {
		return RenderBatchCoordinator{}, v2input.ErrInvalid
	}
	for _, check := range []struct {
		pool *pgxpool.Pool
		role string
	}{{f.API, "photo_api"}, {f.Scheduler, "photo_scheduler"}} {
		var role string
		if err := check.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return RenderBatchCoordinator{}, err
		}
		if role != check.role {
			return RenderBatchCoordinator{}, v2input.ErrInvalid
		}
	}
	store := SQLRenderDraftStore{DB: f.Scheduler, SubmissionSHA: f.SubmissionSHA, SubmissionKey: f.SubmissionKey, QualificationPin: f.QualificationPin}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: f.Scheduler, Bucket: f.DocumentBucket}, Objects: f.Documents}
	return RenderBatchCoordinator{Store: store, Inputs: RenderDraftCoordinator{
		Store: store, Inputs: RenderInputAdmission{DB: f.Scheduler}, Transport: RenderTransportPublisher{Documents: documents},
		Resolver: SQLRenderInputResolver{API: f.API, ProjectID: project, Bucket: f.SourceBucket, Objects: f.Sources},
	}}, nil
}
