package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestAnalysisProjectsRefusesCrossScopeAndDuplicateInstall(t *testing.T) {
	request := analysisPlanFixture(t)
	db := &noAnalysisDraftDB{}
	service := &RelightAnalysisService{tenant: request.Tenant, project: request.Project, coordinator: RelightAnalysisCoordinator{DB: db}}
	service.coordinator.Expected.Operation = request.Operation
	router, err := NewRelightAnalysisProjects(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRelightAnalysisProjects(service, service); err == nil {
		t.Fatal("duplicate service accepted")
	}
	if _, err := NewRelightAnalysisProjects(nil); err == nil {
		t.Fatal("nil service accepted")
	}
	for _, fault := range []string{"tenant", "project", "operation"} {
		tenant, project, operation := request.Tenant, request.Project, request.Operation
		switch fault {
		case "tenant":
			tenant = uuid.New()
		case "project":
			project = uuid.New()
		case "operation":
			operation = "mask.person"
		}
		if _, err := router.SubmitProjectAnalysis(context.Background(), auth.Identity{UserID: request.Actor, SessionID: request.Session}, tenant, project, request.Key, RelightAnalysisIntent{Operation: operation}); err == nil || db.calls != 0 {
			t.Fatal("scope escaped router", fault)
		}
	}
}
