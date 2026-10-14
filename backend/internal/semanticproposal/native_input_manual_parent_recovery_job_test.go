package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestNativeManualParentRecoveryOutcomeRequiresExactCommittedParent(t *testing.T) {
	parent := uuid.New()
	workflow, err := v2input.WorkflowID(parent)
	if err != nil {
		t.Fatal(err)
	}
	ticket := v2input.JobTicket{RunID: parent, WorkflowID: workflow, State: "queued"}
	for _, phase := range []nativeInputPreparationPhase{nativePreparationBlocked, nativePreparationWaiting, nativePreparationFailed, nativePreparationSucceeded} {
		out := nativeManualParentAdvance{phase: phase}
		expected := "waiting"
		if phase == nativePreparationFailed {
			expected = "failed"
		}
		if phase == nativePreparationSucceeded {
			out.ticket = ticket
			expected = "completed"
		}
		if outcome, err := nativeManualParentRecoveryOutcome(parent, out, nil); err != nil || outcome != expected {
			t.Fatal("valid recovery outcome refused", phase, err)
		}
	}
	child := ticket
	child.RunID = uuid.New()
	wrongWorkflow := ticket
	wrongWorkflow.WorkflowID = uuid.New()
	blocked := ticket
	blocked.State = "blocked"
	for _, out := range []nativeManualParentAdvance{{phase: nativePreparationSucceeded}, {phase: nativePreparationSucceeded, ticket: child}, {phase: nativePreparationSucceeded, ticket: wrongWorkflow}, {phase: nativePreparationSucceeded, ticket: blocked}, {phase: nativePreparationWaiting, ticket: ticket}, {phase: nativePreparationFailed, ticket: ticket}, {}} {
		if outcome, err := nativeManualParentRecoveryOutcome(parent, out, nil); err == nil || outcome != "retry" {
			t.Fatal("invalid recovery outcome completed", out.phase)
		}
	}
	failure := errors.New("storage read failed")
	if outcome, err := nativeManualParentRecoveryOutcome(parent, nativeManualParentAdvance{phase: nativePreparationSucceeded, ticket: ticket}, failure); outcome != "retry" || !errors.Is(err, failure) {
		t.Fatal("attempt failure lost", err)
	}
}

func TestNativeManualParentRecoveryCancelledBeforeClaimOrLock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job := SQLNativeManualParentRecovery{}
	if processed, err := job.ProcessOne(ctx); processed || err != context.Canceled {
		t.Fatal("cancelled recovery claimed work", err)
	}
	if out, done, err := job.Recover(ctx, auth.Identity{}, uuid.New(), uuid.New()); done || err != context.Canceled || out.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled recovery exposed ticket", err)
	}
	if done, err := withNativeManualParentLock(ctx, nil, uuid.New(), uuid.New(), nil); done || err != context.Canceled {
		t.Fatal("cancelled lock reached pool", err)
	}
}
