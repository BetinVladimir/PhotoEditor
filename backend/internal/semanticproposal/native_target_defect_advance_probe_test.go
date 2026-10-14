package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func probeNativeTargetDefectAdvance(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, manifest nativeTargetPreparationManifest, ordinal int, owner *pgxpool.Pool) {
	t.Helper()
	factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
	advance := func(want string) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			out, err := p.advanceNativeTargetDefects(work, who, original, manifest, factory)
			if err != nil {
				return err
			}
			if out.phase != nativePreparationWaiting || out.operation != want {
				return v2input.ErrConflict
			}
			return nil
		})
		if err == nil && !ok {
			return v2input.ErrConflict
		}
		return err
	}
	for i := 0; i < 2; i++ {
		if err := advance("defects.propose"); err != nil {
			t.Fatal("target defect queued advance", i, err)
		}
	}
	anchor, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, manifest, ordinal, "defects.propose")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := p.Reader.readNativeTargetDefectSaved(ctx, who, anchor)
	if err != nil || !saved.Sealed || saved.RunState != "queued" {
		t.Fatal("target defect queued saved", saved.RunState, err)
	}
	if probeNativeTargetFailureIfRequested(t, ctx, p, who, original, saved.Run, saved.Task, "defects.propose") {
		return
	}
	probeNativeWireTaskOutputs(t, ctx, saved.Task)
	for i := 0; i < 2; i++ {
		if err := advance("semantic.transfer"); err != nil {
			t.Fatal("target defect completed advance", i, err)
		}
	}
	saved, err = p.Reader.readNativeTargetDefectSaved(ctx, who, anchor)
	if err != nil || saved.RunState != "completed" || saved.TaskState != "succeeded" {
		t.Fatal("target defect completed saved", err)
	}
	var before, after [2]int
	count := func(out *[2]int) {
		t.Helper()
		if err := owner.QueryRow(ctx, "SELECT (SELECT count(*) FROM photo.artifacts WHERE tenant_id=$1),(SELECT count(*) FROM photo.backend_documents WHERE tenant_id=$1)", original.request.TenantID).Scan(&out[0], &out[1]); err != nil {
			t.Fatal(err)
		}
	}
	count(&before)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	revoked := advance("semantic.transfer")
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revoked == nil {
		t.Fatal("revoked target defect advance accepted")
	}
	if err := advance("semantic.transfer"); err != nil {
		t.Fatal("restored target defect advance", err)
	}
	count(&after)
	if before != after {
		t.Fatal("target defect replay published new documents", before, after)
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_PROTECTION") == "1" {
		probeNativeTargetDefectProtection(t, ctx, p, who, original, manifest, ordinal, owner)
	}
	t.Log("V27_TARGET_DEFECT_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK")
}
