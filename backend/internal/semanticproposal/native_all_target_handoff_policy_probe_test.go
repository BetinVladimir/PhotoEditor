package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// All changes are rollback-only; exercise the scheduler boundary, not only the
// internal liveness helper (which deliberately has no policy argument).
func probeNativeAllTargetHandoffPolicy(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) {
	t.Helper()
	begin := func(t *testing.T) (pgx.Tx, uuid.UUID) {
		t.Helper()
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
			t.Fatal(err)
		}
		token := uuid.New()
		changed, err := tx.Exec(ctx, "UPDATE photo.runs SET v27_initial_recovery_done=false,v27_initial_recovery_token=$3,v27_initial_recovery_until=clock_timestamp()+interval '1 minute' WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run, token)
		if err != nil || changed.RowsAffected() != 1 {
			t.Fatal("lease setup", changed, err)
		}
		if _, err := tx.Exec(ctx, "RESET ROLE"); err != nil {
			t.Fatal(err)
		}
		return tx, token
	}
	record := func(t *testing.T, tx pgx.Tx, token uuid.UUID, policy string, want bool) {
		t.Helper()
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
			t.Fatal(err)
		}
		var got bool
		err := tx.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed',$4)", original.request.TenantID, original.shell.Run, token, policy).Scan(&got)
		if err != nil || got != want {
			t.Fatal("policy handoff", got, want, err)
		}
		if _, err := tx.Exec(ctx, "RESET ROLE"); err != nil {
			t.Fatal(err)
		}
		var done bool
		var saved *uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run).Scan(&done, &saved); err != nil {
			t.Fatal(err)
		}
		if want {
			if !done || saved != nil {
				t.Fatal("successful handoff not retired", done, saved)
			}
		} else if done || saved == nil || *saved != token {
			t.Fatal("policy refusal changed scheduling", done, saved)
		}
	}
	for _, op := range []string{"mask.person", "face.detect", "face.landmarks", "face.parse", "defects.propose", "semantic.transfer"} {
		t.Run("revoke_"+op, func(t *testing.T) {
			tx, token := begin(t)
			changed, err := tx.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=(SELECT id FROM photo.v2_qualification_policies WHERE operation=$1 AND admission_enabled ORDER BY id LIMIT 1)", op)
			if err != nil || changed.RowsAffected() != 1 {
				t.Fatal("revoke policy", changed, err)
			}
			record(t, tx, token, original.policy.body, false)
		})
	}
	for _, body := range []string{"", "[]", "{", original.policy.body + " "} {
		t.Run("invalid_or_unbound_snapshot", func(t *testing.T) { tx, token := begin(t); record(t, tx, token, body, false) })
	}
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_POLICY_REVOKE_REFUSED_OK")
	// Locate each report through the exact saved stage, never a source-wide
	// alternate qualification. Immutable import rows are deleted, not edited.
	qualificationMutation := func(kind string) string {
		selection := `SELECT q.id FROM photo.runs stage CROSS JOIN LATERAL jsonb_each(stage.config_snapshot) n
    JOIN photo.tasks task ON task.tenant_id=stage.tenant_id AND task.run_id=stage.id AND task.id::text=n.value->>'task_id'
    JOIN photo.model_qualifications q ON q.tenant_id=stage.tenant_id AND q.report_artifact_id::text=n.value->'recovery'->'qualification_report'->>'artifact_id'
      AND q.report_sha=n.value->'recovery'->'qualification_report'->>'sha256'
    WHERE stage.tenant_id=$1 AND n.value->>'parent_run_id'=$2::text AND task.operation=$3
      AND n.key IN('v27_person_prepare','v27_detector_prepare','v27_face_prepare','v27_defect_native_prepare','v27_target_person_prepare','v27_target_detector_prepare','v27_target_face_prepare','v27_target_defect_prepare')
      AND (n.key LIKE 'v27_target_%')=$4 AND q.trusted_import ORDER BY stage.id LIMIT 1`
		if kind == "import_deleted" {
			return "DELETE FROM photo.model_qualifications WHERE tenant_id=$1 AND id=(" + selection + ")"
		}
		return "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=(SELECT report_artifact_id FROM photo.model_qualifications WHERE tenant_id=$1 AND id=(" + selection + "))"
	}
	for _, target := range []bool{false, true} {
		for _, op := range []string{"mask.person", "face.detect", "face.landmarks", "face.parse", "defects.propose"} {
			for _, kind := range []string{"import_deleted", "report_expired"} {
				name := op + "_" + kind + "_reference"
				if target {
					name = op + "_" + kind + "_target"
				}
				t.Run(name, func(t *testing.T) {
					tx, token := begin(t)
					changed, err := tx.Exec(ctx, qualificationMutation(kind), original.request.TenantID, original.request.RunID, op, target)
					if err != nil || changed.RowsAffected() != 1 {
						t.Fatal("upstream qualification revoke", changed, err)
					}
					record(t, tx, token, original.policy.body, false)
				})
			}
		}
	}
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_UPSTREAM_QUALIFICATION_REVOKE_REFUSED_OK")

	for _, writerCase := range []struct {
		name, sql string
		args      []any
	}{
		{"policy_writer_waits_for_handoff", "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=(SELECT id FROM photo.v2_qualification_policies WHERE operation='semantic.transfer' AND admission_enabled ORDER BY id LIMIT 1)", nil},
		{"qualification_writer_waits_for_handoff", qualificationMutation("import_deleted"), []any{original.request.TenantID, original.request.RunID, "mask.person", true}},
	} {
		t.Run(writerCase.name, func(t *testing.T) {
			reader, token := begin(t)
			// Legacy completion cannot bypass the new snapshot requirement.
			if _, err := reader.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				t.Fatal(err)
			}
			var bypass bool
			if err := reader.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", original.request.TenantID, original.shell.Run, token).Scan(&bypass); err != nil || bypass {
				t.Fatal("legacy policy bypass", bypass, err)
			}
			if _, err := reader.Exec(ctx, "RESET ROLE"); err != nil {
				t.Fatal(err)
			}
			record(t, reader, token, original.policy.body, true)
			writer, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback(context.Background())
			var readerPID, writerPID int
			if err := reader.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&readerPID); err != nil {
				t.Fatal(err)
			}
			if err := writer.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&writerPID); err != nil || readerPID == writerPID {
				t.Fatal("distinct writer session", err)
			}
			if _, err := writer.Exec(ctx, "SET LOCAL lock_timeout='1s'"); err != nil {
				t.Fatal(err)
			}
			mutation := writerCase.sql
			_, err = writer.Exec(ctx, mutation, writerCase.args...)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
				t.Fatal("policy writer not blocked", err)
			}
			if err := writer.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err := reader.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			retry, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer retry.Rollback(context.Background())
			if _, err := retry.Exec(ctx, "SET LOCAL lock_timeout='1s'"); err != nil {
				t.Fatal(err)
			}
			changed, err := retry.Exec(ctx, mutation, writerCase.args...)
			if err != nil || changed.RowsAffected() != 1 {
				t.Fatal("policy writer after release", changed, err)
			}
		})
	}
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_POLICY_WRITER_BLOCKED_OK")
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_QUALIFICATION_WRITER_BLOCKED_OK")
}
