package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestAnalysisQualificationCatalogRefusesInvalidScopeBeforeDatabase(t *testing.T) {
	request := analysisPlanFixture(t)
	who := auth.Identity{UserID: request.Actor, SessionID: request.Session}
	for _, fault := range []string{"operation", "tenant", "project", "actor", "session", "bucket", "report", "cancelled"} {
		db := &noAnalysisDraftDB{}
		catalog := SQLRelightAnalysisQualificationCatalog{DB: db, Bucket: "photos"}
		tenant, project, identity, operation, runtime := request.Tenant, request.Project, who, request.Operation, request.Runtime
		ctx, cancel := context.WithCancel(context.Background())
		switch fault {
		case "operation":
			operation = "image.relight"
		case "tenant":
			tenant = uuid.Nil
		case "project":
			project = uuid.Nil
		case "actor":
			identity.UserID = uuid.Nil
		case "session":
			identity.SessionID = uuid.Nil
		case "bucket":
			catalog.Bucket = ""
		case "report":
			runtime.Qualification.ArtifactID = uuid.Nil
		case "cancelled":
			cancel()
		}
		_, _, err := catalog.ResolveRelightAnalysisQualification(ctx, identity, tenant, project, operation, runtime)
		cancel()
		if err == nil || db.calls != 0 {
			t.Fatal("invalid request reached database", fault, err)
		}
	}
}
