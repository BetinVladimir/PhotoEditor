package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDefectExecutionAuthorizationUsesOriginalScope(t *testing.T) {
	m := manualPinTestProof()
	reader := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	root := m.objects.Root.object
	root.SHA256 = m.binding.Source.SHA256
	pins := []sourcePreparationPin{{Node: v2draft.LayerRef{Ref: m.binding.Source, Contract: "WorkingImage", CanvasHash: m.binding.CanvasHash}, Object: root}, {Node: v2draft.LayerRef{Ref: m.objects.Pixel.ref, Contract: "WorkingPixels", ParentRef: m.binding.Source}, Object: m.objects.Pixel.object}}
	body, err := canonical.Marshal(pins)
	if err != nil {
		t.Fatal(err)
	}
	proof := sourcePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, unitRGB: true, pins: string(body)}
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := reader.lockDefectExecutionAuthorization(context.Background(), tx, who, m.tenant, m.binding, proof); err != nil || len(tx.calls) != 3 {
		t.Fatal("original authorization scope", err, tx.calls)
	}
	for fail := 1; fail <= 3; fail++ {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler", failAt: fail, err: errors.New("injected SQL failure")}}
		if err := reader.lockDefectExecutionAuthorization(context.Background(), tx, who, m.tenant, m.binding, proof); err == nil || len(tx.calls) != fail {
			t.Fatal("authorization continued after failure", fail, err, tx.calls)
		}
	}
	for _, mutate := range []func(*sourcePreparationProof){func(p *sourcePreparationProof) { p.session = uuid.New() }, func(p *sourcePreparationProof) { p.tenant = uuid.New() }, func(p *sourcePreparationProof) { p.unitRGB = false }, func(p *sourcePreparationProof) { p.pins = "[]" }} {
		changed := proof
		mutate(&changed)
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
		if err := reader.lockDefectExecutionAuthorization(context.Background(), tx, who, m.tenant, m.binding, changed); err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid scope reached SQL", err, tx.calls)
		}
	}
}
