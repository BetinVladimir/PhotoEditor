package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type nativeInitialDefectAdvance struct {
	phase      nativeInputPreparationPhase
	protection registeredNativeInputProtection
}

// Caller holds the original parent lock. Completion here is verified internal
// protection readiness, never parent admission or permission to retire recovery.
func (b nativePreparationBootstrap) advanceInitialPersonDefect(ctx context.Context, who auth.Identity, producers nativeInputRestoredFaceProducers, person nativePersonInputAuthority, capacity TransferPreparation, reserved int64, documents v2input.DocumentPublisher) (nativeInitialDefectAdvance, error) {
	var zero nativeInitialDefectAdvance
	submission, err := b.ensureInitialPersonDefect(ctx, who, producers, person, capacity, reserved, documents)
	if err != nil {
		return zero, err
	}
	d := producers.detector
	state, err := b.Reader.readNativeInputSavedStageState(ctx, who, d.request, d.published.Ref, d.anchor.snapshotRef, "defects.propose")
	if err != nil {
		return zero, err
	}
	phase, err := nativeInputStagePreparationPhase(state)
	if err != nil {
		return zero, err
	}
	if phase != nativePreparationSucceeded {
		if phase == nativePreparationFailed {
			return zero, v2input.ErrConflict
		}
		return nativeInitialDefectAdvance{phase: phase}, nil
	}
	// Publisher repeats physical canonical result, immutable object, Params and
	// person coverage checks around publication/registration. SQL stays IO-free.
	built, err := recoverNativeInputDefectTransport(ctx, b.Scheduler, who, submission.plan)
	if err != nil {
		return zero, err
	}
	protection, err := b.Reader.publishRegisteredNativeInputProtection(ctx, b.Scheduler, who, submission.plan, capacity.Color, b.Documents, built)
	if err != nil {
		return zero, err
	}
	if err := b.Reader.recheckNativeDefectPersonInput(ctx, who, submission.plan.prepared.dependencies.preparation); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeInitialDefectAdvance{phase: nativePreparationSucceeded, protection: protection}, nil
}
