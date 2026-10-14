package v2input

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

// SemanticAssemblyRuntime uses a scheduler-role pool and its configured
// versioned object store. It does not enable operations or import qualification.
type SemanticAssemblyRuntime struct {
	DB      *pgxpool.Pool
	Objects *objectstore.Store
	Bucket  string
}

func (r SemanticAssemblyRuntime) Orchestrator() (SemanticAssemblyOrchestrator, error) {
	if r.DB == nil || r.Objects == nil || len(r.Bucket) < 3 || len(r.Bucket) > 63 {
		return SemanticAssemblyOrchestrator{}, ErrInvalid
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: r.DB, Bucket: r.Bucket}, Objects: r.Objects}
	source := SQLSemanticAssemblySource{DB: r.DB, Objects: r.Objects, Bucket: r.Bucket, Qualification: SemanticAssemblyQualification(r.DB, r.Objects, r.Bucket)}
	return SemanticAssemblyOrchestrator{
		Store: SQLSemanticAssemblyStore{DB: r.DB}, Source: source, Documents: documents, Committer: Admission{DB: r.DB},
		TransportForLease: func(l SemanticAssemblyLease) Publisher {
			return ScopedPublisher{Documents: documents, Tenant: l.TenantID, Run: l.RunID, AssemblyLease: l.Token}
		},
	}, nil
}
