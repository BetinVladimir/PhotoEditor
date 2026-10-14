package httpapi

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"testing"
)

func TestMultiTicketChecksOperationAndTaskIdentity(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	run := uuid.New()
	workflow, _ := v2input.WorkflowID(run)
	task, _ := v2input.TaskID(workflow, "optical.panorama", 0)
	for _, fault := range []string{"", "actor", "session", "workflow", "task", "operation", "state"} {
		receipt := v2optical.MultiReceipt{SchemaVersion: 1, Run: run, Workflow: workflow, Task: task, Actor: who.UserID, Session: who.SessionID, State: "queued", Operation: "optical.panorama"}
		switch fault {
		case "actor":
			receipt.Actor = uuid.New()
		case "session":
			receipt.Session = uuid.New()
		case "workflow":
			receipt.Workflow = uuid.New()
		case "task":
			receipt.Task = uuid.New()
		case "operation":
			receipt.Operation = "optical.lens"
		case "state":
			receipt.State = "running"
		}
		_, err := multiJobTicket(receipt, who, "optical.panorama")
		if (err == nil) != (fault == "") {
			t.Fatal(fault, err)
		}
	}
}
