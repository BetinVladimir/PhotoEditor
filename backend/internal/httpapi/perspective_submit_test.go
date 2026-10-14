package httpapi

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"testing"
)

func TestPerspectiveTicketChecksOperationAndTaskIdentity(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	run := uuid.New()
	workflow, _ := v2input.WorkflowID(run)
	task, _ := v2input.TaskID(workflow, "optical.perspective", 0)
	for _, fault := range []string{"", "actor", "session", "workflow", "task", "operation", "state"} {
		receipt := v2optical.PerspectiveReceipt{SchemaVersion: 1, Run: run, Workflow: workflow, Task: task, Actor: who.UserID, Session: who.SessionID, State: "queued", Operation: "optical.perspective"}
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
		_, err := perspectiveJobTicket(receipt, who, v2optical.PerspectiveIntent{})
		if (err == nil) != (fault == "") {
			t.Fatal(fault, err)
		}
	}
}
