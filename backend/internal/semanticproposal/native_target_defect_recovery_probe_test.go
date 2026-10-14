package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

func probeNativeTargetDefectRecovery(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, built v2input.Built, owner *pgxpool.Pool) {
	t.Helper()
	count := func() [2]int {
		var out [2]int
		if err := owner.QueryRow(ctx, "SELECT (SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1),(SELECT count(*) FROM photo.backend_documents WHERE tenant_id=$1)", plan.shell.Tenant).Scan(&out[0], &out[1]); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := count()
	saved, err := p.Reader.readNativeTargetDefectSaved(ctx, who, plan.anchor)
	if err != nil || saved.State != "saved" || !saved.Sealed || saved.RunState != "completed" {
		t.Fatal("target defect saved completed", err)
	}
	f := plan.prepared.preparation
	restore := func(current nativeTargetDefectSaved) (nativeTargetDefectDraftPlan, error) {
		return p.Reader.restoreNativeTargetDefectPlan(ctx, who, f.landmarksPlan, f.skinPlan, f.personPlan, plan.anchor, p.Configuration.Color, current)
	}
	restored, err := restore(saved)
	if err != nil || restored.seal != plan.seal || !reflect.DeepEqual(restored.shell, plan.shell) {
		t.Fatal("target defect restored plan", err)
	}
	for _, kind := range []string{"params", "digest", "config_sha", "profile", "report"} {
		changed := saved
		switch kind {
		case "params":
			changed.Params.SHA256 = "changed"
		case "digest":
			changed.InputDigest = "changed"
		case "config_sha":
			changed.ConfigSHA = "changed"
		case "profile":
			changed.Profile = json.RawMessage(`{"backend":"cuda"}`)
		case "report":
			var config map[string]any
			if err := json.Unmarshal(saved.Config, &config); err != nil {
				t.Fatal(err)
			}
			config["v27_target_defect_prepare"].(map[string]any)["recovery"].(map[string]any)["qualification_report"] = nil
			changed.Config, err = json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := restore(changed); err == nil {
			t.Fatal("target defect substituted saved plan accepted", kind)
		}
	}
	var recovered v2input.Built
	ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
		return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
			var err error
			recovered, err = p.Reader.recoverNativeTargetDefectTransport(work, tx, who, original, restored)
			return err
		})
	})
	if err != nil || !ok || !reflect.DeepEqual(recovered, built) {
		t.Fatal("target defect recovered exact transport", err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	_, revoked := p.Reader.readNativeTargetDefectSaved(ctx, who, plan.anchor)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revoked == nil {
		t.Fatal("target defect revoked saved read accepted")
	}
	if _, err := restore(saved); err != nil {
		t.Fatal("target defect restored session recovery", err)
	}
	if after := count(); after != before {
		t.Fatal("target defect recovery published replacement documents", before, after)
	}
	t.Log("V27_TARGET_DEFECT_SAVED_RECOVERY_TRANSPORT_REPLAY_SUBSTITUTION_REVOKE_OK")
}
