package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
)

func TestDonorQueuedReceiptCommitAndIdentity(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	run := uuid.New()
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.DonorQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, Backend: "cpu"}
	manual := donorManualAdmissionWire{Guard: ref(), Original: ref(), Root: ref(), Target: ref()}
	for _, mode := range []string{"ok", "role", "revoked", "actor", "session", "run", "unknown", "commit", "cancelled", "qualification", "manual", "alias", "vectors", "workflow", "task"} {
		t.Run(mode, func(t *testing.T) {
			workflow := uuid.New()
			task, _ := v2input.TaskID(workflow, "portrait.open_eyes", 0)
			fields := map[string]any{"schema_version": 1, "run_id": run, "workflow_id": workflow, "task_id": task, "actor_id": who.UserID, "auth_session_id": who.SessionID, "state": "queued", "descriptor_sha": strings.Repeat("a", 64), "manual": manual, "qualification": pin}
			switch mode {
			case "workflow":
				fields["workflow_id"] = uuid.New()
			case "task":
				fields["task_id"] = uuid.New()
			case "actor":
				fields["actor_id"] = uuid.New()
			case "session":
				fields["auth_session_id"] = uuid.New()
			case "run":
				fields["run_id"] = uuid.New()
			case "qualification":
				fields["qualification"] = map[string]any{}
			case "manual":
				fields["manual"] = map[string]any{}
			case "alias":
				bad := manual
				bad.Root = pin.Report
				fields["manual"] = bad
			case "vectors":
				bad := manual
				bad.Vectors = &bad.Root
				fields["manual"] = bad
			case "unknown":
				fields["extra"] = true
			}
			raw, _ := canonical.Marshal(fields)
			tx := &donorDescriptorTestTx{role: "photo_scheduler", raw: raw, failCommit: mode == "commit"}
			if mode == "role" {
				tx.role = "photo_api"
			}
			if mode == "revoked" {
				tx.raw = nil
			}
			db := &donorDescriptorTestDB{tx: tx}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			store := SQLDonorDescriptorStore{DB: db}
			ticket, err := store.ReadQueuedTicketForWorkflow(ctx, who, uuid.New(), run, workflow, 0)
			var got []byte
			if err == nil {
				got, err = store.ReadQueuedReceipt(ctx, who, uuid.New(), run)
			}
			if err != nil && ticket != (DonorQueuedTicket{}) {
				t.Fatal("partial ticket")
			}
			if (err == nil) != (mode == "ok") {
				t.Fatal(mode, err)
			}
			if err != nil && got != nil {
				t.Fatal("partial receipt")
			}
			if mode == "ok" {
				if tx.commits != 2 {
					t.Fatal("uncommitted receipt")
				}
				tx.raw[0] = '!'
				if got[0] != '{' {
					t.Fatal("borrowed receipt")
				}
			}
			if mode == "role" && tx.calls != 1 {
				t.Fatal("wrong role read receipt")
			}
			if mode == "cancelled" && db.begins != 0 {
				t.Fatal("cancelled receipt read began")
			}
		})
	}
}
