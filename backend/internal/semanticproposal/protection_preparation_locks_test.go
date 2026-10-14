package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"reflect"
	"strings"
	"testing"
)

func TestProtectionBatchBarrierAndOwnership(t *testing.T) {
	m := manualPinTestProof()
	p := protectionPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, root: m.objects.Root, pixels: m.objects.Pixel}
	s := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := s.lockProtectionPreparationProofs(context.Background(), tx, who, m.tenant, m.binding, []protectionPreparationProof{p, p}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 5 || !strings.Contains(tx.calls[2], "person_inventory") || !reflect.DeepEqual(tx.args[1], tx.args[4]) {
		t.Fatal("barrier/recheck", tx.calls)
	}
	for _, fail := range []int{2, 3, 5} {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := s.lockProtectionPreparationProofs(context.Background(), tx, who, m.tenant, m.binding, []protectionPreparationProof{p}); err == nil || len(tx.calls) != fail {
			t.Fatal("failed phase advanced", fail, err)
		}
	}
	changed := p
	changed.pixels.object.Key += "-changed"
	invalid := p
	invalid.session = uuid.New()
	for _, batch := range [][]protectionPreparationProof{nil, {p, changed}, {p, invalid}, make([]protectionPreparationProof, 65)} {
		tx := &manualLockTx{role: "photo_scheduler"}
		if err := s.lockProtectionPreparationProofs(context.Background(), tx, who, m.tenant, m.binding, batch); err == nil || len(tx.calls) != 0 {
			t.Fatal("invalid batch reached SQL", err)
		}
	}
}
