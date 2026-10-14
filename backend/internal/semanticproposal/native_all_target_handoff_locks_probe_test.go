package semanticproposal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func probeNativeAllTargetHandoffLocks(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) {
	t.Helper()
	for _, test := range nativeHandoffMutationCases {
		t.Run("concurrent_"+test.name, func(t *testing.T) {
			reader, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Rollback(context.Background())
			var ready bool
			var readerPID, writerPID int
			if err := reader.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&readerPID); err != nil {
				t.Fatal(err)
			}
			if err := reader.QueryRow(ctx, "SELECT photo.v27_native_automatic_handoff_ready($1,$2)", original.request.TenantID, original.shell.Run).Scan(&ready); err != nil || !ready {
				t.Fatal("reader baseline", ready, err)
			}
			writer, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback(context.Background())
			if err := writer.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&writerPID); err != nil || readerPID == writerPID {
				t.Fatal("writer must own separate database session", err)
			}
			if _, err := writer.Exec(ctx, "SET LOCAL lock_timeout='1s'"); err != nil {
				t.Fatal(err)
			}
			_, err = writer.Exec(ctx, test.sql, original.request.TenantID, original.request.RunID)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
				t.Fatal("handoff lock did not block concurrent mutation", err)
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
			changed, err := retry.Exec(ctx, test.sql, original.request.TenantID, original.request.RunID)
			if err != nil || changed.RowsAffected() != 1 {
				t.Fatal("writer cannot proceed after handoff lock release", changed, err)
			}
		})
	}
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_CONCURRENT_WRITERS_BLOCKED_OK")
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	token := uuid.New()
	if _, err := tx.Exec(ctx, "UPDATE photo.runs SET v27_initial_recovery_done=false,v27_initial_recovery_token=$3,v27_initial_recovery_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run, token); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	var recorded bool
	if err := tx.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed',$4)", original.request.TenantID, original.shell.Run, token, original.policy.body).Scan(&recorded); err != nil || recorded {
		t.Fatal("expired lease completed", recorded, err)
	}
	if _, err := tx.Exec(ctx, "RESET ROLE"); err != nil {
		t.Fatal(err)
	}
	var done bool
	var saved *uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run).Scan(&done, &saved); err != nil || done || saved == nil || *saved != token {
		t.Fatal("expired lease refusal mutated scheduling", done, saved, err)
	}
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_EXPIRED_LEASE_REFUSED_OK")
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	probeNativeAllTargetHandoffClockExpiry(t, ctx, owner, original)
}

// A row lock cannot stop wall-clock expiry. Keep the same transaction open
// across a positive gate and expiry to detect accidental use of transaction time.
func probeNativeAllTargetHandoffClockExpiry(t *testing.T, ctx context.Context, owner *pgxpool.Pool, original detectorDraftPlan) {
	t.Helper()
	for _, root := range []bool{true, false} {
		name := "lease_clock_expiry"
		if root {
			name = "root_clock_expiry"
		}
		t.Run(name, func(t *testing.T) {
			tx, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if root {
				changed, err := tx.Exec(ctx, `UPDATE photo.artifacts SET expires_at=clock_timestamp()+interval '3 seconds' WHERE tenant_id=$1 AND id=(SELECT (e->'protection'->'ref'->>'artifact_id')::uuid FROM photo.runs r CROSS JOIN LATERAL jsonb_array_elements(r.config_snapshot->'v27_native_targets_transfer'->'targets') e WHERE r.tenant_id=$1 AND r.id=$2 AND e->'reuse_reference'='false'::jsonb LIMIT 1)`, original.request.TenantID, original.request.RunID)
				if err != nil || changed.RowsAffected() != 1 {
					t.Fatal("set future root expiry", changed, err)
				}
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
				t.Fatal(err)
			}
			token := uuid.New()
			ttl := "1 minute"
			if !root {
				ttl = "3 seconds"
			}
			changed, err := tx.Exec(ctx, "UPDATE photo.runs SET v27_initial_recovery_done=false,v27_initial_recovery_token=$3,v27_initial_recovery_until=clock_timestamp()+$4::interval WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run, token, ttl)
			if err != nil || changed.RowsAffected() != 1 {
				t.Fatal("set future lease", changed, err)
			}
			if _, err := tx.Exec(ctx, "RESET ROLE"); err != nil {
				t.Fatal(err)
			}
			var ready, live bool
			if err := tx.QueryRow(ctx, "SELECT photo.v27_native_automatic_handoff_ready($1,$2),v27_initial_recovery_until>clock_timestamp() FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run).Scan(&ready, &live); err != nil || !ready || !live {
				t.Fatal("future TTL baseline", ready, live, err)
			}
			timer := time.NewTimer(3200 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-timer.C:
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				t.Fatal(err)
			}
			var recorded bool
			if err := tx.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed',$4)", original.request.TenantID, original.shell.Run, token, original.policy.body).Scan(&recorded); err != nil || recorded {
				t.Fatal("clock-expired authority completed", recorded, err)
			}
			if _, err := tx.Exec(ctx, "RESET ROLE"); err != nil {
				t.Fatal(err)
			}
			var done bool
			var saved *uuid.UUID
			if err := tx.QueryRow(ctx, "SELECT v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run).Scan(&done, &saved); err != nil || done || saved == nil || *saved != token {
				t.Fatal("clock expiry changed scheduling", done, saved, err)
			}
		})
	}
	t.Log("V27_NATIVE_ALL_TARGET_HANDOFF_CLOCK_EXPIRY_REFUSED_OK")
}
