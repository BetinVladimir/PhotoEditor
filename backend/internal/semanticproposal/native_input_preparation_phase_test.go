package semanticproposal

import (
	"testing"

	"github.com/google/uuid"
)

func TestNativePreparationPhaseUsesPersistedTaskAndRunStates(t *testing.T) {
	for _, test := range []struct {
		task, run string
		sealed    bool
		phase     nativeInputPreparationPhase
		invalid   bool
	}{
		{"blocked", "draft", false, nativePreparationBlocked, false},
		{"ready", "queued", true, nativePreparationWaiting, false},
		{"queued", "running", true, nativePreparationWaiting, false},
		{"running", "running", true, nativePreparationWaiting, false},
		{"retry_wait", "running", true, nativePreparationWaiting, false},
		{"succeeded", "completed", true, nativePreparationSucceeded, false},
		{"succeeded", "running", true, nativePreparationSucceeded, false},
		{"failed", "completed_with_errors", true, nativePreparationFailed, false},
		{"skipped", "running", true, nativePreparationFailed, false},
		{"running", "cancelling", true, nativePreparationFailed, false},
		{"succeeded", "failed", true, nativePreparationFailed, false},
		{"running", "running", false, "", true},
		{"succeeded", "completed", false, "", true},
		{"queued", "completed", true, "", true},
		{"running", "waiting_review", true, "", true},
		{"unknown", "running", true, "", true},
		{"running", "unknown", true, "", true},
	} {
		t.Run(test.task+"/"+test.run, func(t *testing.T) {
			phase, err := nativeInputStagePreparationPhase(nativeInputSavedStageState{task: uuid.New(), taskState: test.task, runState: test.run, sealed: test.sealed})
			if test.invalid {
				if err == nil || phase != "" {
					t.Fatal(phase, err)
				}
				return
			}
			if err != nil || phase != test.phase {
				t.Fatal(phase, err)
			}
		})
	}
}
