package v2render

import (
	"context"
	"errors"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func finalGenerationReadWitnessFixture(t *testing.T) VerifiedAuthoredFinalGeneration {
	t.Helper()
	raw, ref, report, locks := finalGenerationFixture(t)
	p, e := VerifyAuthoredFinalGenerationEvidence(context.Background(), raw, ref, report, locks)
	if e != nil {
		t.Fatal(e)
	}
	d, e := p.Evidence()
	if e != nil {
		t.Fatal(e)
	}
	p.documents = []v2draft.DocumentWitness{
		{Path: "generation", Contract: "AuthoredFinalGenerationEvidence", Ref: ref, JCS: string(raw), Object: objectstore.Object{Key: "generation", VersionID: "v1", SHA256: ref.SHA256, Bytes: int64(len(raw)), ContentType: "application/json"}},
		{Path: "generation.report", Contract: "QualificationReport", Ref: d.Report, Parent: ref, JCS: string(report), Object: objectstore.Object{Key: "report", VersionID: "v2", SHA256: d.Report.SHA256, Bytes: int64(len(report)), ContentType: "application/json"}},
	}
	return p
}
func TestFinal338TaskMountBindsPhysicalGenerationBeforeDigest(t *testing.T) {
	evidence, ref, base := authoredFinalTaskCompilerFixture(t)
	p := finalGenerationReadWitnessFixture(t)
	d, _ := p.Evidence()
	base.Qualification = RenderQualificationPin{RenderQualification: d.Qualification, CalibrationSHA: d.CalibrationSHA}
	base.RuntimeSHA = d.Qualification.RuntimeSHA
	base.ModelSHA = d.Qualification.ModelSHA
	baseRaw, _ := canonical.Marshal(base)
	old, e := compileAuthoredFinalTask(evidence, ref, baseRaw)
	if e != nil {
		t.Fatal(e)
	}
	base.ContractsSHA = AuthoredFinalRoot3SchemasSHA
	raw, _ := canonical.Marshal(AuthoredFinalRoot3ExecutionProfile{AuthoredFinalExecutionProfile: base, GenerationEvidence: p.Ref()})
	plan, e := compileAuthoredFinalRoot3Task(context.Background(), evidence, ref, raw, p)
	if e != nil {
		t.Fatal(e)
	}
	if plan.Task.ID != old.Task.ID || len(plan.Entries) != len(old.Entries)+2 || plan.Task.InputDigest == old.Task.InputDigest || plan.validateFrozen() != nil || old.validateFrozen() != nil {
		t.Fatal("immutable base/generation closure")
	}
	refs, e := v2draft.DocumentChildren([]byte(p.documentJCS), "AuthoredFinalGenerationEvidence", p.Ref())
	if e != nil || len(refs) != 1 || refs[0].Ref != d.Report || refs[0].Contract != "QualificationReport" {
		t.Fatal("typed report edge", e)
	}
	if _, e = v2draft.DocumentChildren([]byte(p.reportJCS), "QualificationReport", d.Report); e != nil {
		t.Fatal("report leaf", e)
	}
}
func TestFinal338MountRefusesUnscopedProofAndCorruptPhysicalWitness(t *testing.T) {
	for _, name := range []string{"no-read", "wrong-version", "wrong-body", "report-parent", "entry-conflict"} {
		t.Run(name, func(t *testing.T) {
			evidence, ref, base := authoredFinalTaskCompilerFixture(t)
			p := finalGenerationReadWitnessFixture(t)
			d, _ := p.Evidence()
			base.Qualification = RenderQualificationPin{RenderQualification: d.Qualification, CalibrationSHA: d.CalibrationSHA}
			base.RuntimeSHA = d.Qualification.RuntimeSHA
			base.ModelSHA = d.Qualification.ModelSHA
			base.ContractsSHA = AuthoredFinalRoot3SchemasSHA
			raw, _ := canonical.Marshal(AuthoredFinalRoot3ExecutionProfile{AuthoredFinalExecutionProfile: base, GenerationEvidence: p.Ref()})
			switch name {
			case "no-read":
				p.documents = nil
			case "wrong-version":
				p.documents[0].Object.VersionID = "null"
			case "wrong-body":
				p.documents[1].JCS = "{}"
			case "report-parent":
				p.documents[1].Parent = ref
			case "entry-conflict":
				evidence.Inputs.Entries = append(evidence.Inputs.Entries, v2input.Entry{Ref: p.Ref(), Contract: "Artifact"})
				evidence.Inputs.Occurrences = append(evidence.Inputs.Occurrences, v2draft.OccurrenceWitness{})
			}
			if _, e := compileAuthoredFinalRoot3Task(context.Background(), evidence, ref, raw, p); e == nil {
				t.Fatal("accepted", name)
			}
		})
	}
}
func TestFinal338PreparationConstructorAndCatalogStayUnavailable(t *testing.T) {
	if _, e := NewSQLAuthoredFinalRoot3Continuation(nil, nil, nil, "", v2input.Ref{}); e == nil {
		t.Fatal("missing actual deployment accepted")
	}
	p := finalGenerationReadWitnessFixture(t)
	d, _ := p.Evidence()
	c := verifiedFinalGenerationCatalog{proof: p}
	a, e := c.ResolveAuthoredFinal(context.Background(), d.Qualification)
	if e != nil {
		t.Fatal(e)
	}
	a.Samples[0].Width = 0
	b, e := c.ResolveAuthoredFinal(context.Background(), d.Qualification)
	if e != nil || b.Samples[0].Width == 0 {
		t.Fatal("catalog aliases")
	}
	wrong := d.Qualification
	wrong.RuntimeSHA = "wrong"
	if _, e = c.ResolveAuthoredFinal(context.Background(), wrong); e == nil {
		t.Fatal("foreign qualification")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = c.ResolveAuthoredFinal(ctx, d.Qualification); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e = compileAuthoredFinalRoot3Task(ctx, AuthoredFinalInputEvidence{}, v2input.Ref{}, nil, p); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
