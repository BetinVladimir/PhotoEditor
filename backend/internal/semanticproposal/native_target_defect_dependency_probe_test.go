package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"testing"
)

func probeNativeTargetDefectDependencies(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, prepared nativeTargetDefectPreparation, plan *nativeTargetDefectDraftPlan, owner *pgxpool.Pool) {
	t.Helper()
	var transports nativeTargetDefectTransports
	parentLock := func(fn func(context.Context) error) {
		t.Helper()
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, fn)
		if err != nil || !ok {
			t.Fatal("target defect dependency parent lock", ok, err)
		}
	}
	parentLock(func(work context.Context) error {
		return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
			var err error
			transports.person, err = p.Reader.recoverNativeTargetPersonTransport(work, tx, who, original, prepared.personPlan)
			if err != nil {
				return err
			}
			transports.detector, err = p.Reader.recoverNativeTargetDetectorTransport(work, tx, who, original, prepared.landmarksPlan.detector)
			if err != nil {
				return err
			}
			transports.landmarks, err = p.Reader.recoverNativeTargetFaceTransport(work, tx, who, original, prepared.landmarksPlan)
			if err != nil {
				return err
			}
			transports.parser, err = p.Reader.recoverNativeTargetFaceTransport(work, tx, who, original, prepared.skinPlan.parser)
			if err != nil {
				return err
			}
			transports.skin, err = p.Reader.recoverNativeTargetSkinTransport(work, tx, who, original, prepared.skinPlan)
			return err
		})
	})
	proof, err := p.Reader.prepareNativeTargetDefectDependencies(ctx, who, original, prepared, p.Configuration.Color, transports)
	if err != nil {
		t.Fatal("target defect physical dependencies", err)
	}
	lock := func(value nativeTargetDefectDependencies) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.lockNativeTargetDefectDependenciesInTransaction(work, tx, who, original, value)
			})
		})
		if err == nil && !ok {
			t.Fatal("target defect dependency lock missing")
		}
		return err
	}
	if err := lock(proof); err != nil {
		t.Fatal("target defect dependency consume", err)
	}
	if err := lock(proof); err != nil {
		t.Fatal("target defect dependency replay", err)
	}
	for _, kind := range []string{"person", "landmarks", "skin", "prepared"} {
		changed := proof
		switch kind {
		case "person":
			changed.person = "{}"
		case "landmarks":
			changed.landmarks = "{}"
		case "skin":
			changed.skin = "{}"
		case "prepared":
			changed.preparation.params.params = "{}"
		}
		if err := lock(changed); err == nil {
			t.Fatal("target defect dependency substitution accepted", kind)
		}
	}
	// A syntactically valid rehashed packet is still rejected by dedicated SQL.
	changed := proof
	changed.skin = "{}"
	changed.seal, err = targetDefectDependencyDigest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock(changed); err == nil {
		t.Fatal("target defect forged consumer packet accepted")
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	revokedErr := lock(proof)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revokedErr == nil {
		t.Fatal("target defect revoked dependencies accepted")
	}
	if err := lock(proof); err != nil {
		t.Fatal("target defect restored dependencies", err)
	}
	for _, run := range []uuid.UUID{prepared.personPlan.person.shell.Run, prepared.landmarksPlan.shell.Run, prepared.skinPlan.shell.Run} {
		if _, err := owner.Exec(ctx, "UPDATE photo.runs SET status='running' WHERE tenant_id=$1 AND id=$2", original.request.TenantID, run); err != nil {
			t.Fatal(err)
		}
		incompleteErr := lock(proof)
		if _, err := owner.Exec(ctx, "UPDATE photo.runs SET status='completed' WHERE tenant_id=$1 AND id=$2", original.request.TenantID, run); err != nil {
			t.Fatal(err)
		}
		if incompleteErr == nil {
			t.Fatal("target defect incomplete producer accepted", run)
		}
		if err := lock(proof); err != nil {
			t.Fatal("target defect restored completed producer", err)
		}
	}

	if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_BEGIN") == "1" {
		if plan == nil {
			t.Fatal("target defect begin plan missing")
		}
		begin := func() (DraftAdmission, error) {
			var result DraftAdmission
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					var err error
					result, err = p.Reader.beginNativeTargetDefectDraftInTransaction(work, tx, who, original, *plan, proof, p.Configuration.Color)
					return err
				})
			})
			if err == nil && !ok {
				t.Fatal("target defect begin parent lock missing")
			}
			return result, err
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		_, revokedErr := begin()
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		if revokedErr == nil {
			t.Fatal("target defect revoked begin accepted")
		}
		for i := 0; i < 2; i++ {
			result, err := begin()
			expected := "created"
			if i == 1 {
				expected = "pending"
			}
			if err != nil || result.RunID != plan.shell.Run || result.Outcome != expected {
				t.Fatal("target defect blocked begin/replay", result, err)
			}
		}
		var status, state string
		var sealed bool
		if err := owner.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", original.request.TenantID, plan.shell.Run).Scan(&status, &sealed, &state); err != nil || status != "draft" || sealed || state != "blocked" {
			t.Fatal("target defect blocked state", status, sealed, state, err)
		}
		if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_TRANSPORT") == "1" {
			probeNativeTargetDefectTransport(t, ctx, p, who, original, *plan, proof, owner)
		}
		t.Log("V27_TARGET_DEFECT_BLOCKED_BEGIN_REPLAY_REVOKE_OK")
	}
	t.Log("V27_TARGET_DEFECT_COMPLETED_DEPENDENCIES_RUNNING_REFUSAL_OK")
	t.Log("V27_TARGET_DEFECT_DEPENDENCY_LOCK_REPLAY_REVOKE_OK")
}
