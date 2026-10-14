package httpapi

import (
	"github.com/google/uuid"
	"net/http/httptest"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"testing"
)

func TestRelightAnalysisRouteRequiresAuthentication(t *testing.T) {
	s := &Server{Auth: &auth.Service{Pepper: []byte("analysis-route")}}
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/tenants/"+uuid.NewString()+"/projects/"+uuid.NewString()+"/relight-analyses", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestAnalysisJobTicketBindsOperationAndTask(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	run := uuid.New()
	workflow, _ := v2input.WorkflowID(run)
	task, _ := v2input.TaskID(workflow, "depth.predict", 0)
	intent := v2portrait.RelightAnalysisIntent{Operation: "depth.predict"}
	original := v2portrait.RelightAnalysisReceipt{SchemaVersion: 1, Run: run, Workflow: workflow, Task: task, State: "queued", Actor: who.UserID, Session: who.SessionID, Operation: intent.Operation}
	if ticket, err := analysisJobTicket(original, who, intent); err != nil || ticket.RunID != run {
		t.Fatal(err)
	}
	for _, fault := range []string{"actor", "session", "workflow", "task", "operation", "state"} {
		changed := original
		switch fault {
		case "actor":
			changed.Actor = uuid.New()
		case "session":
			changed.Session = uuid.New()
		case "workflow":
			changed.Workflow = uuid.New()
		case "task":
			changed.Task = uuid.New()
		case "operation":
			changed.Operation = "mask.person"
		case "state":
			changed.State = "running"
		}
		if _, err := analysisJobTicket(changed, who, intent); err == nil {
			t.Fatal("accepted", fault)
		}
	}
}
