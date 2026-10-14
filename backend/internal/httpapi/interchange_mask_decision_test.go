package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/interchange"
)

type interchangeMaskDecisionStub struct {
	calls int
	bad   bool
}

func (s *interchangeMaskDecisionStub) Decide(_ context.Context, _ auth.Identity, _, _, workflow uuid.UUID, req interchange.ImportPersonMaskDecisionRequest) (interchange.ImportPersonMaskDecisionReceipt, error) {
	s.calls++
	if s.bad {
		workflow = uuid.New()
	}
	return interchange.ImportPersonMaskDecisionReceipt{WorkflowID: workflow, EntryID: req.EntryID, Status: "revoked", Epoch: 2}, nil
}

func TestInterchangeMaskDecisionHTTPChecksReceipt(t *testing.T) {
	tenant, project, workflow, entry := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	request := `{"action":"revoke","entry_id":"` + entry.String() + `","target_version_id":"` + uuid.NewString() + `","override_artifact_id":"` + uuid.NewString() + `","mask_artifact_id":"` + uuid.NewString() + `"}`
	for _, bad := range []bool{false, true} {
		stub := &interchangeMaskDecisionStub{bad: bad}
		response := httptest.NewRecorder()
		serveInterchangeMaskDecision(response, httptest.NewRequest("POST", "/", strings.NewReader(request)), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, project, workflow, stub)
		expected := 200
		if bad {
			expected = 503
		}
		if response.Code != expected || stub.calls != 1 {
			t.Fatalf("status=%d calls=%d", response.Code, stub.calls)
		}
	}
}

func TestInterchangeMaskDecisionRouteRequiresSession(t *testing.T) {
	server := &Server{Auth: &auth.Service{Pepper: []byte("mask-confirmation")}}
	response := httptest.NewRecorder()
	server.Routes().ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/interchange-jobs/"+uuid.NewString()+"/person-mask-confirmations", strings.NewReader(`{}`)))
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
}
