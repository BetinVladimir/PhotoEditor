package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDefectOriginEvidenceLockScopeAndFailures(t *testing.T) {
	m := manualPinTestProof()
	p := defectOriginPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding}
	refs := [5]v2input.Ref{}
	for i := range refs {
		refs[i] = v2input.Ref{ArtifactID: uuid.New(), SHA256: m.binding.Source.SHA256}
		media, size := "application/json", int64(100)
		if i >= 3 {
			media = "application/octet-stream"
		}
		p.objects[i] = defectEvidenceObject{ref: refs[i], object: objectstore.Object{Key: uuid.NewString(), VersionID: "v1", SHA256: refs[i].SHA256, Bytes: size, ContentType: media}}
	}
	p.pins = DefectOriginPins{Params: refs[0], Proposal: refs[1], Report: refs[2], Labels: refs[3], Protection: refs[4]}
	reader := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := reader.lockDefectOriginPreparationProof(context.Background(), tx, who, m.tenant, m.binding, p); err != nil || len(tx.calls) != 3 {
		t.Fatal("original origin lock", err, tx.calls)
	}
	for fail := 1; fail <= 3; fail++ {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler", failAt: fail, err: errors.New("injected failure")}}
		if err := reader.lockDefectOriginPreparationProof(context.Background(), tx, who, m.tenant, m.binding, p); err == nil || len(tx.calls) != fail {
			t.Fatal("origin lock continued after failure", fail, err, tx.calls)
		}
	}
	for _, mutate := range []func(*defectOriginPreparationProof){
		func(p *defectOriginPreparationProof) { p.tenant = uuid.New() },
		func(p *defectOriginPreparationProof) { p.session = uuid.New() },
		func(p *defectOriginPreparationProof) { p.objects[0].object.VersionID = "null" },
		func(p *defectOriginPreparationProof) { p.objects[3].object.ContentType = "application/json" },
		func(p *defectOriginPreparationProof) { p.objects[4] = p.objects[3] },
	} {
		bad := p
		mutate(&bad)
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
		if err := reader.lockDefectOriginPreparationProof(context.Background(), tx, who, m.tenant, m.binding, bad); err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid origin reached SQL", err, tx.calls)
		}
	}
}
