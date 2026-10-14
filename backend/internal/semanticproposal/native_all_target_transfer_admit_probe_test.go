package semanticproposal

import (
	"context"
	"errors"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

func probeNativeAllTargetTransferAdmission(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation) {
	t.Helper()
	owned, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
		if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_CHECKPOINT_JOB") == "1" && !nativePartialPublicationEnabled() {
			return nil
		}
		if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_FIRST") == "1" && os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_JOB") != "1" {
			manifest, err := compileNativeTargetPreparationManifest(work, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody)
			if err != nil {
				return err
			}
			factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
			ticket, err := p.admitPreparedNativeAllTargetParent(work, who, original, nativeInitialDefectAdvance{phase: nativePreparationSucceeded, protection: plan.reference.person.source.protection}, manifest, factory)
			if err != nil || ticket.RunID != original.request.RunID || ticket.WorkflowID != original.request.WorkflowID || ticket.State != "queued" {
				t.Fatal("first automatic all-native parent", ticket, err)
			}
			t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_FIRST_PARENT_OK")
			return nil
		}
		defect := plan.reference.person.source.protection.published.preparation.plan
		built, err := recoverNativeInputDefectTransport(work, p.Scheduler, who, defect)
		if err != nil {
			return err
		}
		publisher := v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: original.request.TenantID, Run: original.request.RunID}
		if nativePartialPublicationEnabled() {
			interrupted := &nativePartialPublishProbe{publisher: publisher, lostAck: nativePartialLostAckEnabled()}
			prepared, err := p.Reader.persistNativeAllTargetTransfer(work, p.Scheduler, interrupted, who, original.request, original.requestKey, original.published, plan, c, built, p.ReservedBytes)
			if !errors.Is(err, errNativePartialPublishProbe) || prepared != nil || interrupted.calls != 2 {
				t.Fatal("publication was not interrupted", err)
			}
			probeNativeExpiredTransferReservation(t, work, p, who, original, plan, c, built)
			if nativePartialLostAckEnabled() {
				t.Log("V27_NATIVE_ALL_TARGET_LOST_PUBLICATION_ACK_INJECTED_OK")
			}
			t.Log("V27_NATIVE_ALL_TARGET_PARTIAL_PUBLICATION_INTERRUPTED_OK")
			t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_JOB_BLOCKED_PARENT_OK")
			return nil
		}
		if nativeMixedRegistrationEnabled() {
			prepareNativeMixedRegistration(t, work, p, who, original, plan, c, built)
			return nil
		}
		if nativeRegistrationCommitAckEnabled() {
			probeNativeRegistrationCommitAckLoss(t, work, p, who, original, plan, c, built, publisher)
			return nil
		}
		prepared, err := p.Reader.persistNativeAllTargetTransfer(work, p.Scheduler, publisher, who, original.request, original.requestKey, original.published, plan, c, built, p.ReservedBytes)
		if err != nil {
			return err
		}
		if len(prepared) != len(plan.targets) {
			t.Fatal("incomplete durable transfer transport")
		}
		raw, sha, err := p.Reader.readNativeAllTargetTransferSaved(work, who, original.request)
		if err != nil {
			return err
		}
		if _, err := p.Reader.decodeNativeAllTargetTransferSaved(work, who, original.request, raw, sha); err != nil {
			return err
		}
		if sha != plan.core.configSHA {
			t.Fatal("persisted transfer config changed")
		}
		if nativeSealCommitAckEnabled() {
			probeNativeSealCommitAckLoss(t, work, p, who, original, plan, c, built)
			return nil
		}
		if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_JOB") == "1" {
			t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_JOB_BLOCKED_PARENT_OK")
			return nil
		}
		if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_ROUTE") == "1" {
			ticket, err := p.admitInitialPersonDefectParent(work, who, nativeInitialDefectAdvance{phase: nativePreparationSucceeded, protection: plan.reference.person.source.protection})
			if err != nil || ticket.RunID != original.request.RunID || ticket.WorkflowID != original.request.WorkflowID || ticket.State != "queued" {
				t.Fatal("saved all-native parent route", ticket, err)
			}
			t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_SAVED_PARENT_ROUTE_OK")
			return nil
		}
		if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_LOAD") == "1" {
			return nil
		}
		reconstructor := TransferDraftPreparer{Reader: p.Reader, Scheduler: p.Scheduler}
		factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
		catalog, err := factory(work, who, original.request.TenantID, p.Reader.ProjectID, original.request.Intent.Reference.AssetVersionID)
		if err != nil {
			return err
		}
		restoredOriginal, restoredReference, restoredBuilt, err := reconstructor.restoreNativeAllTargetTransferOriginal(work, catalog, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, c.Color)
		if err != nil {
			return err
		}
		if restoredOriginal.shell.ConfigSHA != original.shell.ConfigSHA {
			t.Fatal("original detector changed during saved recovery")
		}
		t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_ORIGINAL_RESTORE_OK")
		restored, capacity, err := reconstructor.restoreNativeAllTargetTransferPlan(work, who, restoredOriginal, restoredReference, c.Color, factory)
		if err != nil {
			return err
		}
		if restored.core.configSHA != plan.core.configSHA || !reflect.DeepEqual(restored.core.tasks, plan.core.tasks) || !reflect.DeepEqual(capacity, c) {
			t.Fatal("saved graph/capacity restoration changed frozen plan")
		}
		original = restoredOriginal
		built = restoredBuilt
		plan = restored
		c = capacity
		t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_SAVED_PLAN_RESTORE_OK")
		var first v2input.JobTicket
		for i := 0; i < 2; i++ {
			ticket, err := p.Reader.resumeNativeAllTargetTransfer(work, p.Scheduler, who, original.request, original.requestKey, original.published, plan, c, built)
			if err != nil {
				return err
			}
			if ticket.RunID != original.request.RunID || ticket.State != "queued" {
				t.Fatal("wrong durable native transfer ticket")
			}
			if i == 0 {
				first = ticket
			} else if first != ticket {
				t.Fatal("native transfer replay ticket changed")
			}
		}
		return nil
	})
	if err != nil || !owned {
		t.Fatal("durable all-native transfer admission", owned, err)
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_JOB") == "1" {
		probeNativeAllTargetRecoveryJob(t, ctx, p, original)
		return
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_FIRST") == "1" {
		return
	}

	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_ROUTE") == "1" {
		return
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_PROCESS") == "1" {
		probeNativeAllTargetTransferProcess(t, ctx, p, who, original, c.Color)
		return
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_LOAD") == "1" {
		reconstructor := TransferDraftPreparer{Reader: p.Reader, Scheduler: p.Scheduler}
		ticket, err := reconstructor.loadAndResumeNativeAllTargetTransfer(ctx, who, original.request.TenantID, original.requestKey, original.request.Body, c.Color)
		if err != nil || ticket.RunID != original.request.RunID || ticket.State != "queued" {
			t.Fatal("server-loaded native transfer", ticket, err)
		}
		t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_SERVER_LOAD_OK")
		return
	}
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_DURABLE_ADMISSION_REPLAY_OK")
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_REGISTERED_RECOVERY_OK")
}
