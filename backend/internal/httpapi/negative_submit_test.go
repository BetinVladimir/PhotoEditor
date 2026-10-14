package httpapi

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"testing"
)

func TestNegativeTicketChecksOperationAndTaskIdentity(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	run := uuid.New()
	workflow, _ := v2input.WorkflowID(run)
	task, _ := v2input.TaskID(workflow, "optical.negative", 0)
	calibration:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	for _, fault := range []string{"", "actor", "session", "workflow", "task", "operation", "state", "calibration"} {
		receipt := v2optical.NegativeReceipt{SchemaVersion: 1, Run: run, Workflow: workflow, Task: task, Actor: who.UserID, Session: who.SessionID, State: "queued", Operation: "optical.negative"}
		receipt.Qualification.Calibration=calibration
		switch fault {
 case "calibration":receipt.Qualification.Calibration.ArtifactID=uuid.New()
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
		_, err := negativeJobTicket(receipt, who, v2optical.NegativeIntent{Params:v2optical.NegativeParams{Calibration:calibration}})
		if (err == nil) != (fault == "") {
			t.Fatal(fault, err)
		}
	}
}
