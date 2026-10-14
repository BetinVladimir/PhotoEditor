package v2learning

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// TrainingFitRuntime provides the scheduler-role, object-backed admission
// components used after a TrainProfile request has resolved its frozen cases.
type TrainingFitRuntime struct {
	DB      *pgxpool.Pool
	Objects *objectstore.Store
	Bucket  string
}

func (r TrainingFitRuntime) Orchestrator(draft TrainingDraftPrepared,
	frozen map[uuid.UUID]CompiledTrainingRecipes) (
	TrainingFitOrchestrator, error) {
	if r.DB == nil || r.Objects == nil || r.Bucket == "" ||
		draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil {
		return TrainingFitOrchestrator{}, ErrInvalidTrainingDraft
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{
		DB: r.DB, Bucket: r.Bucket}, Objects: r.Objects}
	return TrainingFitOrchestrator{
		Stager: TrainingInputStager{DB: r.DB, Publisher: documents,
			FrozenRecipes: frozen},
		Transport: v2input.ScopedPublisher{Documents: documents,
			Tenant: draft.TenantID, Run: draft.RunID},
		Committer: v2input.Admission{DB: r.DB},
	}, nil
}

// EvaluateOrchestrator installs the lease-scoped frozen readers and transport
// for the scheduler's second V23 graph expansion. No editor session is reused.
func (r TrainingFitRuntime) EvaluateOrchestrator() (
	LearningEvaluateOrchestrator, error) {
	if r.DB == nil || r.Objects == nil || r.Bucket == "" {
		return LearningEvaluateOrchestrator{}, ErrInvalidTrainingDraft
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{
		DB: r.DB, Bucket: r.Bucket}, Objects: r.Objects}
	leaseStore := SQLLearningEvaluateStore{DB: r.DB}
	return LearningEvaluateOrchestrator{
		Leases: leaseStore,
		Source: FrozenLearningEvaluateSource{
			Manifests: SQLLearningEvaluateManifestSource{DB: r.DB,
				Objects: r.Objects, Bucket: r.Bucket},
			Renders: FrozenLearningRenderResolver{
				Roots: SQLLearningEvaluateRootSource{DB: r.DB,
					Objects: r.Objects, Bucket: r.Bucket},
				Images: SQLLearningEvaluateImageSource{DB: r.DB, Bucket: r.Bucket},
				Masks:  SQLLearningEvaluateMaskSource{DB: r.DB, Bucket: r.Bucket},
				Styles: SQLLearningEvaluateStyleSource{DB: r.DB,
					Objects: r.Objects, Bucket: r.Bucket},
				Consents: SQLLearningEvaluateConsentPinner{DB: r.DB},
				Defects: SQLFrozenEvaluateDefectReader{DB: r.DB,
					Objects: r.Objects, Bucket: r.Bucket},
				Dust: SQLFrozenEvaluateDustReader{DB: r.DB,
					Objects: r.Objects, Bucket: r.Bucket},
				Detail: SQLFrozenEvaluateDetailReader{DB: r.DB,
					Objects: r.Objects, Bucket: r.Bucket},
			},
		},
		Stager: TrainingInputStager{DB: r.DB, Publisher: documents},
		TransportForLease: func(lease LearningEvaluateLease) v2input.Publisher {
			return v2input.ScopedPublisher{Documents: documents,
				Tenant: lease.TenantID, Run: lease.RunID}
		},
		Committer: leaseStore,
	}, nil
}
