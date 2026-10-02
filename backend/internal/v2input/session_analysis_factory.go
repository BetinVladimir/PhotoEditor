package v2input

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

// SessionAnalysisRuntime wires the trusted V22 admission phases to one
// scheduler-role pool. API authentication and project authorization remain at
// the caller boundary; SQL repeats them with the authenticated session ID.
type SessionAnalysisRuntime struct {
	DB             *pgxpool.Pool
	Objects        *objectstore.Store
	Bucket         string
	Backend        string
	RAMLimitBytes  int64
	VRAMLimitBytes int64
}

func (r SessionAnalysisRuntime) Coordinator() (SessionAnalysisCoordinator, error) {
	if r.DB == nil || r.Objects == nil || r.Bucket == "" ||
		(r.Backend != "cuda" && r.Backend != "cpu" && r.Backend != "mps") ||
		r.RAMLimitBytes < 2<<30 ||
		(r.Backend != "cpu" && r.VRAMLimitBytes < 6<<30) {
		return SessionAnalysisCoordinator{}, ErrInvalid
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{
		DB: r.DB, Bucket: r.Bucket}, Objects: r.Objects}
	selection := SQLSessionAnalysisSelectionSource{DB: r.DB,
		Objects: r.Objects, Bucket: r.Bucket}
	consentSource := SQLSessionConsentSource{DB: r.DB}
	pending := SessionAnalysisOrchestrator{
		Documents: documents,
		TransportForDraft: func(draft SessionAnalysisDraft) Publisher {
			return ScopedPublisher{Documents: documents, Tenant: draft.Tenant,
				Run: draft.Run}
		},
		Images: SQLSessionAnalysisImageSource{DB: r.DB,
			Objects: r.Objects, Bucket: r.Bucket},
		Consents: SQLSessionConsentPinner{DB: r.DB, Source: consentSource,
			Bucket: r.Bucket},
		Stage:     SQLAnalysisStageStore{DB: r.DB},
		Committer: Admission{DB: r.DB},
	}
	beginner := SQLSessionAnalysisBegin{DB: r.DB, Selection: selection}
	return SessionAnalysisCoordinator{
		Beginner: beginner,
		Prober:   beginner,
		Pending:  pending,
		Profiles: SQLSessionDescribeProfiles{DB: r.DB, Objects: r.Objects,
			Bucket: r.Bucket, Backend: r.Backend,
			RAMLimitBytes: r.RAMLimitBytes, VRAMLimitBytes: r.VRAMLimitBytes},
		Tickets: SQLAdmissionStore{DB: r.DB},
	}, nil
}
