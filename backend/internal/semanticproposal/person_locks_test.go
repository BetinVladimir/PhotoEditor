package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"reflect"
	"strings"
	"testing"
)

func TestPersonBatchLocksBeforeSingleBarrier(t *testing.T) {
	m := manualPinTestProof()
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	protected := protectionPreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, root: m.objects.Root, pixels: m.objects.Pixel}
	first := personProtectionProof{Binding: m.binding, Root: v2input.Ref{ArtifactID: uuid.MustParse("ffffffff-ffff-4fff-8fff-ffffffffffff"), SHA256: m.objects.Root.ref.SHA256}, Child: m.objects.Pixel.ref, Protection: protected.root.ref, Count: 1, protectionEvidence: protected}
	first.Child.ArtifactID = uuid.New()
	first.Report = v2input.Ref{ArtifactID: uuid.New(), SHA256: m.objects.Root.ref.SHA256}
	expected, _ := canonical.Marshal(v2qualification.Expected{Operation: "mask.person", Version: "1", Backend: "cpu"})
	first.Expected = string(expected)
	evidence := []personEvidencePin{{Contract: "PersonParams", ArtifactID: uuid.New(), SHA256: first.Root.SHA256, Bucket: "bucket", Key: "params", Version: "v1", Bytes: 2, Backend: "cpu"}, {Contract: "PersonReport", ArtifactID: uuid.New(), SHA256: first.Root.SHA256, Bucket: "bucket", Key: "report", Version: "v1", Bytes: 2, Backend: "cpu"}}
	body, _ := canonical.Marshal(evidence)
	first.Evidence = string(body)
	qualification, _ := canonical.Marshal(personEvidencePin{Contract: "QualificationReport", ArtifactID: first.Report.ArtifactID, SHA256: first.Report.SHA256, Bucket: "bucket", Key: "qualification", Version: "v1", Bytes: 2, Backend: "cpu"})
	first.Qualification = string(qualification)
	second := first
	second.Root.ArtifactID = uuid.MustParse("00000000-0000-4000-8000-000000000001")
	request := Request{TenantID: m.tenant, ActorID: m.actor, RunID: uuid.New()}
	plan := WorkflowPlan{tenant: m.tenant, actor: m.actor, project: m.project, run: request.RunID, tasks: []v2input.DraftTask{{}}, person: []personProtectionProof{first, second}}
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := LockPersonProducerInTransaction(context.Background(), tx, who, request, plan); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 20 || !strings.Contains(tx.calls[9], "person_inventory") || tx.args[3][7] != second.Root.ArtifactID || tx.args[7][7] != first.Root.ArtifactID {
		t.Fatal("all locks/order before barrier", tx.calls)
	}
	for _, pair := range [][2]int{{2, 11}, {3, 12}, {4, 13}, {6, 15}, {7, 16}, {8, 17}} {
		if !reflect.DeepEqual(tx.args[pair[0]], tx.args[pair[1]]) {
			t.Fatal("repeat changed evidence")
		}
	}
	for _, fail := range []int{3, 4, 5, 7, 8, 9, 10, 12, 13, 14, 16, 17, 18, 19, 20} {
		tx := &manualLockTx{role: "photo_scheduler", failAt: fail}
		if err := LockPersonProducerInTransaction(context.Background(), tx, who, request, plan); err == nil || len(tx.calls) != fail {
			t.Fatal("failed phase continued", fail, err)
		}
	}
	invalid := second
	invalid.protectionEvidence.session = uuid.New()
	plan.person = []personProtectionProof{first, invalid}
	tx = &manualLockTx{role: "photo_scheduler"}
	if err := LockPersonProducerInTransaction(context.Background(), tx, who, request, plan); err == nil || len(tx.calls) != 0 {
		t.Fatal("invalid later proof reached SQL", err)
	}
	for name, mutate := range map[string]func(*personProtectionProof){
		"noncanonical expected": func(p *personProtectionProof) { p.Expected = " " + p.Expected },
		"unknown expected field": func(p *personProtectionProof) {
			p.Expected = strings.TrimSuffix(p.Expected, "}") + `,"unexpected":true}`
		},
		"missing evidence":             func(p *personProtectionProof) { p.Evidence = "[]" },
		"noncanonical evidence":        func(p *personProtectionProof) { p.Evidence = " " + p.Evidence },
		"wrong qualification identity": func(p *personProtectionProof) { p.Report.ArtifactID = uuid.New() },
		"aliased child":                func(p *personProtectionProof) { p.Child = p.Root },
		"null object version": func(p *personProtectionProof) {
			p.Evidence = strings.ReplaceAll(p.Evidence, `"version":"v1"`, `"version":"null"`)
		},
		"wrong bucket": func(p *personProtectionProof) {
			p.Evidence = strings.ReplaceAll(p.Evidence, `"bucket":"bucket"`, `"bucket":"foreign"`)
		},
		"wrong backend": func(p *personProtectionProof) {
			p.Evidence = strings.ReplaceAll(p.Evidence, `"backend":"cpu"`, `"backend":"cuda"`)
		},
		"malformed qualification": func(p *personProtectionProof) { p.Qualification = "{}" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := second
			mutate(&changed)
			plan.person = []personProtectionProof{first, changed}
			tx := &manualLockTx{role: "photo_scheduler"}
			if err := LockPersonProducerInTransaction(context.Background(), tx, who, request, plan); err != v2input.ErrInvalid || len(tx.calls) != 0 {
				t.Fatal("invalid person proof reached SQL", err, tx.calls)
			}
		})
	}

}
