package semanticproposal

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func probeNativeAllTargetRecoveryJob(t *testing.T, ctx context.Context, p TransferDraftPreparer, original detectorDraftPlan) {
	t.Helper()
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("owner observer")
	}
	defer owner.Close()
	var enabled, done bool
	var token *uuid.UUID
	if err := owner.QueryRow(ctx, "SELECT v27_initial_recovery_enabled,v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run).Scan(&enabled, &done, &token); err != nil || !enabled || done || token != nil {
		t.Fatal("unexpected initial recovery scheduling", enabled, done, token, err)
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_CHECKPOINT_JOB") == "1" {
		var exists bool
		if err := owner.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=$1 AND id=$2)", original.request.TenantID, original.request.RunID).Scan(&exists); err != nil || exists {
			t.Fatal("checkpoint job requires absent parent", exists, err)
		}
		t.Log("V27_NATIVE_ALL_TARGET_CHECKPOINT_JOB_PARENT_ABSENT_OK")
	}
	var partial nativePartialPublicationObservation
	if nativePartialPublicationEnabled() {
		partial = observeNativePartialPublication(t, ctx, owner, original)
		// Recovery must use the durable reservation, not this changed default.
		p.ReservedBytes *= 2
	}
	var mixedRegistration string
	if nativeMixedRegistrationEnabled() {
		mixedRegistration = observeNativeMixedParent(t, ctx, owner, original, false)
		p.ReservedBytes *= 2
	}
	var committedRegistration string
	if nativeRegistrationCommitAckEnabled() {
		committedRegistration = observeNativeCommittedRegistration(t, ctx, owner, original)
		p.ReservedBytes *= 2
	}
	var committedSeal string
	var replayPublisher *nativeRefusedRepairPublisher
	if nativeSealCommitAckEnabled() {
		committedSeal = observeNativeCommittedSeal(t, ctx, owner, original)
		replayPublisher = &nativeRefusedRepairPublisher{}
		p.TransportDocuments = replayPublisher
	}
	if nativeRegistrationCommitAckEnabled() {
		replayPublisher = &nativeRefusedRepairPublisher{}
		p.TransportDocuments = replayPublisher
	}
	var recorded bool
	if err := p.Scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", original.request.TenantID, original.shell.Run, uuid.New()).Scan(&recorded); err != nil || recorded {
		t.Fatal("unowned lease completed", recorded, err)
	}
	factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
	job := SQLNativeInitialPipelineRecovery{Preparer: p, CatalogForReference: factory, Timeout: 240 * time.Second}
	if worked, err := job.ProcessOne(ctx); err != nil || !worked {
		t.Fatal("all-native recovery job", worked, err)
	}
	if err := owner.QueryRow(ctx, "SELECT v27_initial_recovery_enabled,v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=$1 AND id=$2", original.request.TenantID, original.shell.Run).Scan(&enabled, &done, &token); err != nil || !enabled || !done || token != nil {
		t.Fatal("handoff not durably recorded", enabled, done, token, err)
	}
	if worked, err := job.ProcessOne(ctx); err != nil || worked {
		t.Fatal("completed recovery reclaimed", worked, err)
	}
	if nativePartialPublicationEnabled() {
		assertNativePartialPublicationRecovered(t, ctx, owner, original, partial)
	}
	if nativeSealCommitAckEnabled() {
		if replayPublisher.calls != 0 {
			t.Fatal("committed seal replay invoked publisher", replayPublisher.calls)
		}
		if after := observeNativeCommittedSeal(t, ctx, owner, original); after != committedSeal {
			t.Fatal("committed seal replay changed parent state")
		}
		t.Log("V27_NATIVE_ALL_TARGET_SEALED_COMMIT_DURABLE_REPLAY_OK")
	}
	if nativeMixedRegistrationEnabled() {
		if observeNativeMixedParent(t, ctx, owner, original, false) != mixedRegistration {
			t.Fatal("mixed repair changed existing registration/documents/reservation")
		}
		t.Log("V27_NATIVE_ALL_TARGET_MIXED_REGISTRATION_RECOVERED_OK")
	}
	if nativeRegistrationCommitAckEnabled() {
		if replayPublisher.calls != 0 || observeNativeRegistrationDocuments(t, ctx, owner, original) != committedRegistration {
			t.Fatal("committed registration replay republished or changed documents/inputs/reservation")
		}
		t.Log("V27_NATIVE_ALL_TARGET_REGISTRATION_COMMIT_DURABLE_REPLAY_OK")
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_HANDOFF") == "1" {
		probeNativeAllTargetHandoffLifecycle(t, ctx, owner, original)
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_HANDOFF_LOCKS") == "1" {
		probeNativeAllTargetHandoffLocks(t, ctx, owner, original)
		probeNativeAllTargetHandoffPolicy(t, ctx, owner, original)
	}
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_RECOVERY_JOB_HANDOFF_OK")
}
