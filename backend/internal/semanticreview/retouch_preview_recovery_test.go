package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestRetouchRecoveryPreservesOwnIdentityAndNeverRestartsSealedRun(t *testing.T) {
	run, target := uuid.New(), uuid.New()
	task := uuid.NewSHA1(run, []byte("semantic.preview_retouch_region:"+target.String()))
	for status, taskState := range map[string]string{"queued": "ready", "running": "running", "completed": "succeeded", "failed": "failed", "cancelled": "cancelled", "cancelling": "running"} {
		state := PreviewInspection{RunID: run, TaskID: task, RunStatus: status, TaskState: taskState, GraphSealed: true, TaskPresent: true}
		got, resume, err := classifyPreviewRecovery(state)
		if err != nil || resume || !got.Recovered || got.RunID != run || got.TaskID != task || got.RunStatus != status {
			t.Fatal("sealed request restarted or lost identity", status, got, resume, err)
		}
	}
	state := PreviewInspection{RunID: run, TaskID: task, RunStatus: "draft"}
	got, resume, err := classifyPreviewRecovery(state)
	if err != nil || !resume || got.RunID != run {
		t.Fatal("empty draft cannot resume", got, resume, err)
	}
	state.TaskPresent = true
	state.TaskState = "blocked"
	if _, resume, err = classifyPreviewRecovery(state); err == nil || resume {
		t.Fatal("partial draft resumed")
	}
	state = PreviewInspection{RunID: run, TaskID: task, RunStatus: "completed", GraphSealed: true}
	if _, resume, err = classifyPreviewRecovery(state); err == nil || resume {
		t.Fatal("missing canonical task treated as rollback")
	}
}
