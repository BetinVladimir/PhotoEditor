package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

func probeNativeOriginalReferenceConsumer(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation) {
	t.Helper()
	reference := plan.reference
	registered := reference.person.source.protection
	defect := registered.published.preparation.plan
	built, err := recoverNativeInputDefectTransport(ctx, p.Scheduler, who, defect)
	if err != nil {
		t.Fatal("original consumer recover transport", err)
	}
	packet, err := p.Reader.marshalNativeDefectCompletedEvidence(ctx, who, defect, c.Color, registered.published.preparation.result, built)
	if err != nil {
		t.Fatal("original consumer physical packet", err)
	}
	locked := func(ref nativeInputTransferReferenceDocuments, body json.RawMessage) error {
		owned, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.lockNativeAllTargetTransferReference(work, tx, who, original.request, ref, c, body)
			})
		})
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("original parent lock unavailable")
		}
		return nil
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var runState string
	if err := owner.QueryRow(ctx, "SELECT status FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, defect.shell.Run).Scan(&runState); err != nil {
		t.Fatal(err)
	}
	t.Log("original canonical defect run state", runState)
	for i := 0; i < 2; i++ {
		if err := locked(reference, packet); err != nil {
			t.Fatal("exact original reference consumer replay", err)
		}
	}
	owned, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
		return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
			if _, err := tx.Exec(work, "SELECT set_config('app.tenant_id',$1,true)", original.request.TenantID.String()); err != nil {
				return err
			}
			args := []any{original.request.TenantID, p.Reader.ProjectID, who.UserID, who.SessionID, original.request.RunID, original.request.RequestSHA, defect.shell.Tasks[0].ID, string(packet)}
			var ready bool
			if err := tx.QueryRow(work, "SELECT photo.lock_semantic_native_defect_result($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", args...).Scan(&ready); err != nil {
				return err
			}
			if ready {
				return fmt.Errorf("legacy unique-candidate gate accepted shared-source original")
			}
			for i := 0; i < 7; i++ {
				changed := append([]any(nil), args...)
				if i == 5 {
					changed[i] = "changed"
				} else {
					changed[i] = uuid.New()
				}
				if err := tx.QueryRow(work, "SELECT photo.lock_semantic_native_original_defect_result($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", changed...).Scan(&ready); err != nil {
					return err
				}
				if ready {
					return fmt.Errorf("foreign original scope accepted at argument %d", i)
				}
			}
			return nil
		})
	})
	if err != nil || !owned {
		t.Fatal("original SQL scope and legacy isolation", err)
	}
	for _, field := range []string{"result_id", "outputs", "params", "files", "person", "config_text", "policy"} {
		var changed map[string]any
		if err := json.Unmarshal(packet, &changed); err != nil {
			t.Fatal(err)
		}
		switch field {
		case "person", "config_text", "policy":
			changed["admission"].(map[string]any)[field] = nil
		default:
			changed[field] = nil
		}
		body, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := locked(reference, body); err == nil {
			t.Fatal("changed original consumer packet accepted", field)
		}
	}
	changed := reference
	changed.person.person.root.ArtifactID = uuid.New()
	if err := locked(changed, packet); err == nil {
		t.Fatal("substituted original person accepted")
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=$1 AND id=$2", original.request.TenantID, defect.shell.Run); err != nil {
		t.Fatal(err)
	}
	cancelRefusal := locked(reference, packet)
	if _, err := owner.Exec(ctx, "UPDATE photo.runs SET cancel_requested=false WHERE tenant_id=$1 AND id=$2", original.request.TenantID, defect.shell.Run); err != nil {
		t.Fatal(err)
	}
	if cancelRefusal == nil {
		t.Fatal("cancelled original producer accepted")
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	refusal := locked(reference, packet)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if refusal == nil {
		t.Fatal("revoked original consumer accepted")
	}
	if err := locked(reference, packet); err != nil {
		t.Fatal("restored original consumer", err)
	}
	lockAll := func(candidate nativeAllTargetTransferWorkflowPlan) error {
		owned, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.lockNativeAllTargetTransferAdmissionEvidence(work, tx, who, original.request, original.published, candidate, c, packet)
			})
		})
		if err != nil {
			return err
		}
		if !owned {
			return fmt.Errorf("all-native parent lock unavailable")
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := lockAll(plan); err != nil {
			t.Fatal("complete all-native admission barrier replay", err)
		}
	}
	for i, target := range plan.targets {
		if target.reuseReference {
			continue
		}
		changed := plan
		changed.targets = append([]nativeTransferTargetProof(nil), plan.targets...)
		changed.targets[i].completed = "{}"
		changed.targets[i].seal, err = nativeTransferTargetDigest(changed.targets[i])
		if err != nil {
			t.Fatal(err)
		}
		if err := lockAll(changed); err == nil {
			t.Fatal("foreign target completed packet accepted", i)
		}
	}
	preparedTasks, err := PublishWorkflowTasks(ctx, plan.core, &proposalMemoryTransport{})
	if err != nil {
		t.Fatal(err)
	}
	transports := make([]v2input.Built, len(preparedTasks))
	for i, task := range preparedTasks {
		transports[i] = task.Graph.Built
	}
	graph, err := BindWorkflowTransport(ctx, plan.core, transports)
	if err != nil {
		t.Fatal(err)
	}
	rollbackProbe := errors.New("rollback all-native blocked begin probe")
	owned, beginErr := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
		return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
			for i := 0; i < 2; i++ {
				outcome, err := p.Reader.beginNativeAllTargetTransferInTransaction(work, tx, who, original.request, original.requestKey, original.published, plan, c, packet, p.ReservedBytes)
				if err != nil {
					return err
				}
				if outcome.RunID != original.request.RunID {
					return fmt.Errorf("wrong blocked parent")
				}
			}
			for i, task := range graph {
				state, err := registrationState(work, tx, original.request, plan.core.tasks[i], task)
				if err != nil {
					return err
				}
				if state != "missing" {
					return fmt.Errorf("blocked all-native task state %s", state)
				}
			}
			return rollbackProbe
		})
	})
	if !errors.Is(beginErr, rollbackProbe) {
		t.Fatal("all-native blocked begin/replay rollback", owned, beginErr)
	}
	var parentRows int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.request.RunID).Scan(&parentRows); err != nil || parentRows != 0 {
		t.Fatal("rolled back all-native begin persisted parent", parentRows, err)
	}
	_, missingErr := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
		return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
			ticket, err := p.Reader.recoverNativeAllTargetTransferInTransaction(work, tx, who, original.request, original.requestKey, original.published, plan, c, packet)
			if ticket != (v2input.JobTicket{}) {
				t.Fatal("missing transfer exposed ticket")
			}
			return err
		})
	})
	if !errors.Is(missingErr, v2input.ErrConflict) {
		t.Fatal("missing registered transfer not refused", missingErr)
	}
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_BLOCKED_BEGIN_ROLLBACK_OK")
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_ADMISSION_BARRIER_OK")
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_ORIGINAL_REFERENCE_SQL_OK")
}
