package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestInitialPipelineOutcomesRefuseIncompleteAuthority(t *testing.T) {
	parent := uuid.New()
	for _, phase := range []nativeInputPreparationPhase{nativePreparationWaiting, nativePreparationBlocked, nativePreparationFailed} {
		out := nativeFacePipelineAdvance{phase: phase, operation: "face.parse"}
		expected := "waiting"
		if phase == nativePreparationFailed {
			expected = "failed"
		}
		got, err := nativeInitialPipelineRecoveryOutcome(parent, out, nil)
		if err != nil || got != expected {
			t.Fatal("valid internal phase refused", got, err)
		}
		out.producers.parser.seal = "unverified"
		if got, err := nativeInitialPipelineRecoveryOutcome(parent, out, nil); err == nil || got != "retry" {
			t.Fatal("pending producer authority accepted")
		}
	}
	for _, out := range []nativeFacePipelineAdvance{{}, {phase: nativePreparationSucceeded}, {phase: nativePreparationWaiting}, {phase: nativePreparationFailed, operation: "semantic.transfer"}} {
		if got, err := nativeInitialPipelineRecoveryOutcome(parent, out, nil); err == nil || got != "retry" {
			t.Fatal("incomplete outcome accepted")
		}
	}
	failure := errors.New("attempt failed")
	if got, err := nativeInitialPipelineRecoveryOutcome(parent, nativeFacePipelineAdvance{}, failure); got != "retry" || !errors.Is(err, failure) {
		t.Fatal("attempt failure lost")
	}
}
func TestInitialPipelineRecoveryCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job := SQLNativeInitialPipelineRecovery{}
	if worked, err := job.ProcessOne(ctx); worked || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled processor claimed work", err)
	}
	if worked, err := job.ProcessOne(context.Background()); worked || err == nil {
		t.Fatal("missing dependencies claimed work")
	}
	var dispatcher *SQLNativeRecoveryDispatcher
	if worked, err := dispatcher.ProcessOne(ctx); worked || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled combined dispatcher accessed queues")
	}
	if worked, err := dispatcher.ProcessOne(context.Background()); worked || err == nil {
		t.Fatal("nil dispatcher claimed work")
	}
}

func TestInitialRuntimeStartCancellationBeforeCatalog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worked, err := (SQLNativeInitialPipelineRecovery{}).StartManual(ctx, auth.Identity{}, uuid.New(), "", nil, .8, 20, nil, [2]float64{})
	if worked || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled runtime start reached catalog", err)
	}
}

func TestInitialPersonPendingRetainsCheckpoint(t *testing.T) {
	for _, state := range []nativeInputSavedStageState{
		{task: uuid.New(), taskState: "blocked", runState: "draft"},
		{task: uuid.New(), taskState: "queued", runState: "queued", sealed: true},
		{task: uuid.New(), taskState: "running", runState: "running", sealed: true},
		{task: uuid.New(), taskState: "retry_wait", runState: "running", sealed: true},
	} {
		pending, err := nativeInitialPersonPending(state)
		if err != nil || pending == nil {
			t.Fatal("pending person lost scheduling checkpoint", state, err)
		}
		outcome, err := nativeInitialPipelineRecoveryOutcome(uuid.New(), *pending, nil)
		if err != nil || outcome != "waiting" {
			t.Fatal("pending person recorded completed", outcome, err)
		}
		pending.producers.parser.seal = "unverified"
		if _, err := nativeInitialPipelineRecoveryOutcome(uuid.New(), *pending, nil); err == nil {
			t.Fatal("pending person carried unverified face authority")
		}
	}
	for _, state := range []nativeInputSavedStageState{
		{},
		{task: uuid.New(), taskState: "succeeded", runState: "completed"},
	} {
		if pending, err := nativeInitialPersonPending(state); err == nil || pending != nil {
			t.Fatal("invalid or failed person accepted", state, err)
		}
	}
	pending, err := nativeInitialPersonPending(nativeInputSavedStageState{task: uuid.New(), taskState: "succeeded", runState: "completed", sealed: true})
	if err != nil || pending != nil {
		t.Fatal("succeeded state was treated as pending", err)
	}
	for _, state := range []nativeInputSavedStageState{
		{task: uuid.New(), taskState: "failed", runState: "failed", sealed: true},
		{task: uuid.New(), taskState: "cancelled", runState: "cancelled", sealed: true},
		{task: uuid.New(), taskState: "skipped", runState: "running", sealed: true},
	} {
		terminal, err := nativeInitialPersonPending(state)
		if err != nil || terminal == nil || terminal.phase != nativePreparationFailed || terminal.operation != "mask.person" {
			t.Fatal("terminal person lost", state, err)
		}
		if outcome, err := nativeInitialPipelineRecoveryOutcome(uuid.New(), *terminal, nil); err != nil || outcome != "failed" {
			t.Fatal("terminal person retries", outcome, err)
		}
		terminal.producers.parser.seal = "not-output-authority"
		if _, err := nativeInitialPipelineRecoveryOutcome(uuid.New(), *terminal, nil); err == nil {
			t.Fatal("terminal person granted producer authority")
		}
	}
}

func TestInitialTargetCheckpointRemainsWaiting(t *testing.T) {
	for _, phase := range []nativeInputPreparationPhase{nativePreparationWaiting, nativePreparationBlocked} {
		out := nativeFacePipelineAdvance{phase: phase, operation: "semantic.transfer"}
		if got, err := nativeInitialPipelineRecoveryOutcome(uuid.New(), out, nil); err != nil || got != "waiting" {
			t.Fatal("target preparation retired recovery", got, err)
		}
		out.producers.detector.seal = "not-authority"
		if got, err := nativeInitialPipelineRecoveryOutcome(uuid.New(), out, nil); err == nil || got != "retry" {
			t.Fatal("waiting carried fabricated authority", got, err)
		}
	}
}
