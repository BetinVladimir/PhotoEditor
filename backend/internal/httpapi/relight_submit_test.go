package httpapi

import (
	"github.com/google/uuid"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRelightRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("relight-route-test")}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/relight-intents", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestRelightTicketBindsIdentityAndWorkflow(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	run := uuid.New()
	workflow, _ := v2input.WorkflowID(run)
	fields := map[string]any{"schema_version": 1, "run_id": run, "workflow_id": workflow, "actor_id": who.UserID, "auth_session_id": who.SessionID, "state": "queued"}
	raw, _ := canonical.Marshal(fields)
	if ticket, err := relightTicket(raw, who); err != nil || ticket.RunID != run {
		t.Fatal(err)
	}
	for _, key := range []string{"actor_id", "auth_session_id", "workflow_id", "state", "schema_version"} {
		copyFields := map[string]any{}
		for k, v := range fields {
			copyFields[k] = v
		}
		copyFields[key] = "invalid"
		bad, _ := canonical.Marshal(copyFields)
		if _, err := relightTicket(bad, who); err == nil {
			t.Fatal("invalid receipt", key)
		}
	}
}

func TestRelightCandidateRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("relight-candidates-test")}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/relight-candidates", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
