package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferTicketRefusesForeignAndNonQueued(t *testing.T) {
	r := Request{RunID: uuid.New(), WorkflowID: uuid.New()}
	ticket := v2input.JobTicket{RunID: r.RunID, WorkflowID: r.WorkflowID, State: "queued"}
	raw, _ := json.Marshal(ticket)
	if got, err := decodeNativeInputTransferTicket(raw, r); err != nil || got != ticket {
		t.Fatal("valid ticket refused", err)
	}
	for _, mode := range []string{"run", "workflow", "state", "empty", "malformed"} {
		changed := ticket
		switch mode {
		case "run":
			changed.RunID = uuid.New()
		case "workflow":
			changed.WorkflowID = uuid.New()
		case "state":
			changed.State = "running"
		}
		raw, _ := json.Marshal(changed)
		if mode == "empty" {
			raw = nil
		}
		if mode == "malformed" {
			raw = []byte("{")
		}
		got, err := decodeNativeInputTransferTicket(raw, r)
		if !errors.Is(err, v2input.ErrInvalid) || got.RunID != uuid.Nil {
			t.Fatalf("accepted %s: %v", mode, err)
		}
	}
}

func TestNativeInputTransferSealCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	got, err := reader.sealNativeInputTransferInTransaction(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if !errors.Is(err, context.Canceled) || got.RunID != uuid.Nil {
		t.Fatalf("cancelled seal: %v", err)
	}
	got, err = reader.activateNativeInputTransfer(context.Background(), nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, v2input.Built{}, nil)
	if !errors.Is(err, v2input.ErrInvalid) || got.RunID != uuid.Nil {
		t.Fatalf("missing scheduler: %v", err)
	}
}
