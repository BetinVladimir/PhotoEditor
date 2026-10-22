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
	epoch int64
}

func (s *interchangeMaskDecisionStub) Decide(_ context.Context, _ auth.Identity, _, _, workflow uuid.UUID, req interchange.ImportPersonMaskDecisionRequest) (interchange.ImportPersonMaskDecisionReceipt, error) {
	s.calls++
	if s.bad {
		workflow = uuid.New()
	}
	epoch := s.epoch
	if epoch == 0 {
		epoch = 2
	}
	return interchange.ImportPersonMaskDecisionReceipt{WorkflowID: workflow, EntryID: req.EntryID, Status: "revoked", Epoch: epoch}, nil
}

func TestInterchangeMaskDecisionHTTPChecksReceipt(t *testing.T) {
	tenant, project, workflow, entry := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	request := `{"action":"revoke","expected_epoch":1,"entry_id":"` + entry.String() + `","target_version_id":"` + uuid.NewString() + `","override_artifact_id":"` + uuid.NewString() + `","mask_artifact_id":"` + uuid.NewString() + `"}`
	for _, bad := range []bool{false, true} {
		stub := &interchangeMaskDecisionStub{bad: bad}
		response := httptest.NewRecorder()
		serveInterchangeMaskDecision(response, newJSONTestRequest("POST", "/", strings.NewReader(request)), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, project, workflow, stub)
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
	server.Routes().ServeHTTP(response, newJSONTestRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/interchange-jobs/"+uuid.NewString()+"/person-mask-confirmations", strings.NewReader(`{}`)))
	if response.Code != 401 {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestInterchangeMaskDecisionHTTPRejectsUnsafeEpoch(t *testing.T) {
	entry := uuid.New()
	request := `{"action":"revoke","expected_epoch":1,"entry_id":"` + entry.String() + `"}`
	stub := &interchangeMaskDecisionStub{epoch: 9007199254740992}
	response := httptest.NewRecorder()
	serveInterchangeMaskDecision(response, newJSONTestRequest("POST", "/", strings.NewReader(request)), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), stub)
	if response.Code != 503 {
		t.Fatalf("unsafe receipt status=%d", response.Code)
	}
}

func TestInterchangeMaskDecisionHTTPRequiresExpectedEpochBeforeService(t *testing.T) {
	for _, body := range []string{`{"action":"revoke"}`, `{"action":"revoke","expected_epoch":null}`, `{"action":"revoke","expected_epoch":-1}`, `{"action":"revoke","expected_epoch":9007199254740991}`} {
		stub := &interchangeMaskDecisionStub{}
		response := httptest.NewRecorder()
		serveInterchangeMaskDecision(response, newJSONTestRequest("POST", "/", strings.NewReader(body)), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), stub)
		if response.Code != 422 || stub.calls != 0 {
			t.Fatalf("status=%d calls=%d", response.Code, stub.calls)
		}
	}
}
func TestInterchangeMaskDecisionHTTPRejectsNonSuccessorReceipt(t *testing.T) {
	for _, epoch := range []int64{1, 3, 9007199254740991} {
		stub := &interchangeMaskDecisionStub{epoch: epoch}
		response := httptest.NewRecorder()
		body := `{"action":"revoke","expected_epoch":1,"entry_id":"` + uuid.NewString() + `"}`
		serveInterchangeMaskDecision(response, newJSONTestRequest("POST", "/", strings.NewReader(body)), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), stub)
		if response.Code != 503 || stub.calls != 1 {
			t.Fatalf("epoch=%d status=%d calls=%d", epoch, response.Code, stub.calls)
		}
	}
}
