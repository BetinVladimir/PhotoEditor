package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestRetainedDefectFileDescriptorsCannotChangeSourceClosure(t *testing.T) {
	m := manualPinTestProof()
	reader := SQLTransferSourceReader{ProjectID: m.project, Bucket: "bucket"}
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	expected := v2qualification.Expected{Operation: "defects.propose", Version: "1", Backend: "cpu"}
	encode := func(v any) string {
		t.Helper()
		b, err := canonical.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	root := m.objects.Root.object
	root.SHA256 = m.binding.Source.SHA256
	nodes := []v2draft.LayerRef{{Ref: m.binding.Source, Contract: "WorkingImage", CanvasHash: m.binding.CanvasHash}, {Ref: m.objects.Pixel.ref, Contract: "WorkingPixels", ParentRef: m.binding.Source}}
	source := sourcePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, iccSHA: m.binding.Source.SHA256, unitRGB: true}
	source.pins = encode([]sourcePreparationPin{{Node: nodes[0], Object: root}, {Node: nodes[1], Object: m.objects.Pixel.object}})
	params := v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}
	published := PublishedDefectParams{Ref: params, Inputs: []review.ArtifactRef{{ID: nodes[0].Ref.ArtifactID, SHA256: nodes[0].Ref.SHA256}, {ID: nodes[1].Ref.ArtifactID, SHA256: nodes[1].Ref.SHA256}}}
	nodes = append(nodes, v2draft.LayerRef{Ref: params, Contract: "Params"})
	descriptors := struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}{published, nodes}
	files := []v2draft.InputEvidence{{Ref: nodes[0].Ref, Contract: nodes[0].Contract, Object: root}, {Ref: nodes[1].Ref, Contract: nodes[1].Contract, Object: m.objects.Pixel.object}, {Ref: params, Contract: "Params", Object: m.objects.Root.object}}
	q := personEvidenceObject{contract: "QualificationReport", ref: m.objects.Root.ref, object: m.objects.Root.object, backend: "cpu"}
	prepared := defectSourceExecutionPreparation{source: source, expected: encode(expected), descriptors: encode(descriptors), files: encode(files), qualification: defectQualificationPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", expected: encode(expected), report: q}}
	if got, err := reader.decodeDefectExecutionFilePins(context.Background(), who, m.tenant, m.binding, prepared); err != nil || len(got) != 3 {
		t.Fatal("original closure", err)
	}

	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := reader.lockDefectExecutionSourceInTransaction(context.Background(), tx, who, m.tenant, m.binding, prepared); err != nil {
		t.Fatal("source qualification composition", err)
	}
	if len(tx.calls) != 6 || !strings.Contains(tx.calls[3], "source_evidence") || !strings.Contains(tx.calls[5], "defect_qualification") {
		t.Fatal("lock composition order", tx.calls)
	}
	for fail := 1; fail <= 6; fail++ {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler", failAt: fail, err: errors.New("injected lock failure")}}
		if err := reader.lockDefectExecutionSourceInTransaction(context.Background(), tx, who, m.tenant, m.binding, prepared); err == nil || len(tx.calls) != fail {
			t.Fatal("continued after lock failure", fail, err, tx.calls)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func([]v2draft.LayerRef)
	}{
		{"parent", func(v []v2draft.LayerRef) { v[1].ParentRef.ArtifactID = uuid.New() }},
		{"canvas", func(v []v2draft.LayerRef) { v[0].CanvasHash = m.objects.Pixel.ref.SHA256 }},
		{"path", func(v []v2draft.LayerRef) { v[1].Path = "different.source" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := descriptors
			changed.Nodes = append([]v2draft.LayerRef(nil), nodes...)
			test.mutate(changed.Nodes)
			bad := prepared
			bad.descriptors = encode(changed)
			if got, err := reader.decodeDefectExecutionFilePins(context.Background(), who, m.tenant, m.binding, bad); err != v2input.ErrConflict || got != nil {
				t.Fatal("changed closure accepted", err)
			}
		})
	}
}
