package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func probeNativeTargetDefectConsumer(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, built v2input.Built, completed completedNativeTargetDefect, owner *pgxpool.Pool) {
	t.Helper()
	packet, err := p.Reader.marshalNativeTargetDefectCompletedEvidence(ctx, who, original, plan, dependencies, p.Configuration.Color, completed, built)
	if err != nil {
		t.Fatal("target defect completed packet", err)
	}
	again, err := p.Reader.marshalNativeTargetDefectCompletedEvidence(ctx, who, original, plan, dependencies, p.Configuration.Color, completed, built)
	if err != nil || string(again) != string(packet) {
		t.Fatal("target defect completed packet replay", err)
	}
	lock := func(body []byte) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.lockNativeCompletedTargetDefectInTransaction(work, tx, who, plan, body)
			})
		})
		if err == nil && !ok {
			t.Fatal("target defect consumer parent lock missing")
		}
		return err
	}
	if err := lock(packet); err != nil {
		t.Fatal("target defect completed SQL lock", err)
	}
	if err := lock(packet); err != nil {
		t.Fatal("target defect completed SQL replay", err)
	}
	for _, kind := range []string{"admission", "result_id", "outputs", "params", "files", "schema_version", "version", "role"} {
		var changed map[string]any
		if err := json.Unmarshal(packet, &changed); err != nil {
			t.Fatal(err)
		}
		if kind == "version" || kind == "role" {
			changed["files"].([]any)[0].(map[string]any)[kind] = "changed"
		} else {
			changed[kind] = nil
		}
		body, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := lock(body); err == nil {
			t.Fatal("target defect substituted consumer packet accepted", kind)
		}
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	revoked := lock(packet)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revoked == nil {
		t.Fatal("target defect revoked consumer accepted")
	}
	if err := lock(packet); err != nil {
		t.Fatal("target defect restored consumer", err)
	}
	t.Log("V27_TARGET_DEFECT_CONSUMER_PACKET_LOCK_REPLAY_SUBSTITUTION_REVOKE_OK")
}
