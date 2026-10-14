package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"strings"
	"testing"
)

func TestFacePreparationBatchBarrier(t *testing.T) {
	m := manualPinTestProof()
	p := facePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, binding: m.binding, bucket: "verified-bucket"}
	for i := range p.objects {
		for j := range p.objects[i] {
			p.objects[i][j] = defectEvidenceObject{ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, object: objectstore.Object{Key: uuid.NewString(), VersionID: "original-version", SHA256: strings.Repeat("c", 64), Bytes: 100, ContentType: "application/json"}}
		}
	}
	var expected [2]v2qualification.Expected
	for i, op := range [2]string{"face.landmarks", "face.detect"} {
		expected[i] = v2qualification.Expected{Operation: op, Version: "1", Backend: "cpu"}
		q := p.objects[i][0]
		q.ref.ArtifactID = uuid.New()
		p.qualification[i] = personEvidenceObject{contract: "QualificationReport", ref: q.ref, object: q.object, backend: "cpu"}
	}
	body, _ := canonical.Marshal(expected)
	p.expected = string(body)
	s := SQLTransferSourceReader{ProjectID: p.project, Bucket: p.bucket}
	who := auth.Identity{UserID: p.actor, SessionID: p.session}
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFacePreparationProofs(context.Background(), tx, who, p.tenant, p.binding, []facePreparationProof{p, p}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 11 || !strings.Contains(tx.calls[5], "person_inventory") {
		t.Fatal("missing dedup/barrier", tx.calls)
	}
	for _, pair := range [][2]int{{1, 7}, {3, 9}, {4, 10}} {
		if tx.calls[pair[0]] != tx.calls[pair[1]] {
			t.Fatal("missing repeat")
		}
	}
	for _, fail := range []int{2, 4, 5, 6, 8, 10, 11} {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := s.lockFacePreparationProofs(context.Background(), tx, who, p.tenant, p.binding, []facePreparationProof{p}); err == nil || len(tx.calls) != fail {
			t.Fatal("failed phase continued", fail, err)
		}
	}
	second := p
	for i := range second.objects {
		for j := range second.objects[i] {
			second.objects[i][j].ref.ArtifactID = uuid.New()
		}
		second.qualification[i].ref.ArtifactID = uuid.New()
	}
	p.objects[0][0].ref.ArtifactID = uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff")
	second.objects[0][0].ref.ArtifactID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := s.lockFacePreparationProofs(context.Background(), tx, who, p.tenant, p.binding, []facePreparationProof{p, second, p}); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 21 || !strings.Contains(tx.calls[10], "person_inventory") || tx.args[1][7] != second.objects[0][0].ref.ArtifactID || tx.args[6][7] != p.objects[0][0].ref.ArtifactID {
		t.Fatal("multi-root order/barrier changed", tx.calls)
	}
	for _, pair := range [][2]int{{1, 12}, {3, 14}, {4, 15}, {6, 17}, {8, 19}, {9, 20}} {
		if tx.calls[pair[0]] != tx.calls[pair[1]] || !reflect.DeepEqual(tx.args[pair[0]], tx.args[pair[1]]) {
			t.Fatal("multi-root recheck pins changed")
		}
	}
	changed := p
	changed.objects[0][1].object.Key += "-changed"
	invalid := p
	invalid.expected = "{}"
	for _, batch := range [][]facePreparationProof{nil, {p, changed}, {p, invalid}, make([]facePreparationProof, 65)} {
		tx := &manualLockTx{role: "photo_scheduler"}
		if err := s.lockFacePreparationProofs(context.Background(), tx, who, p.tenant, p.binding, batch); err == nil || len(tx.calls) != 0 {
			t.Fatal("invalid batch reached SQL", err)
		}
	}
}
