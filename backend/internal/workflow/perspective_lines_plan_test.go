package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func perspectiveLinesFixture(t *testing.T, alpha bool) (PerspectiveLinesScope, []byte, string) {
	t.Helper()
	bundle := perspectivePlanBundle(t)
	pin := bundle.Source()
	raw := bundle.Documents()[pin.Base.Source.ArtifactID.String()]
	if alpha {
		image, err := review.ParseWorkingImage(raw)
		if err != nil {
			t.Fatal(err)
		}
		image.Alpha = &review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("e", 64)}
		raw, err = canonical.Marshal(image)
		if err != nil {
			t.Fatal(err)
		}
		pin.Base.Source.SHA256 = renderDerivativeBytesSHA(raw)
	}
	return PerspectiveLinesScope{Tenant: pin.Tenant, Project: pin.Project, Run: uuid.New(), Base: pin.Base}, raw, strings.Repeat("c", 64)
}
func TestPerspectiveLinesPlanPinsOriginalAndOwnsData(t *testing.T) {
	for _, alpha := range []bool{false, true} {
		scope, raw, icc := perspectiveLinesFixture(t, alpha)
		original := bytes.Clone(raw)
		plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
		if err != nil {
			t.Fatal(err)
		}
		again, err := CompilePerspectiveLinesPlan(scope, raw, icc)
		if err != nil || again.Task() != plan.Task() {
			t.Fatal("unstable task", err)
		}
		params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
		task, err := PreparePerspectiveLinesTask(plan, params)
		if err != nil {
			t.Fatal(err)
		}
		count := 2
		perPixel := int64(12)
		if alpha {
			count = 3
			perPixel = 16
		}
		if task.ID != plan.Task() || task.Operation != "optical.perspective_lines" || task.Version != "1" || task.ResourceClass != "cpu" || task.TimeoutSeconds != 600 || len(task.Inputs) != count || task.Inputs[0].ID != scope.Base.Source.ArtifactID {
			t.Fatal("incorrect command", task)
		}
		estimate, err := ParseResourceEstimate(task.ExecutionProfile)
		if err != nil || estimate.PixelCount != 12 || estimate.RAMBytes != 12*perPixel+268435456 || estimate.ScratchBytes != 12*perPixel+33554432 || estimate.OutputBytes != 1048576 || estimate.VRAMBytes != 0 {
			t.Fatal("worker admission mismatch", estimate, err)
		}
		var body perspectiveLinesParams
		if json.Unmarshal(plan.Params(), &body) != nil || body.SchemaVersion != 1 || body.Base != scope.Base {
			t.Fatal("source binding lost")
		}
		raw[0] = 'x'
		exposed := plan.Params()
		exposed[0] = 'x'
		profile := plan.Profile()
		profile[0] = 'x'
		refs := plan.Inputs()
		refs[0].ID = uuid.New()
		if !bytes.Equal(plan.image, original) || !bytes.Equal(plan.Params(), again.Params()) || !bytes.Equal(plan.Profile(), again.Profile()) || plan.Inputs()[0] != again.Inputs()[0] {
			t.Fatal("mutable plan exposure")
		}
	}
}
func TestPerspectiveLinesPlanRejectsWrongSourceAndICC(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, false)
	for _, alter := range []func(*PerspectiveLinesScope){
		func(s *PerspectiveLinesScope) { s.Tenant = uuid.Nil },
		func(s *PerspectiveLinesScope) { s.Base.AssetVersionID = uuid.Nil },
		func(s *PerspectiveLinesScope) { s.Base.CanvasHash = strings.Repeat("f", 64) },
		func(s *PerspectiveLinesScope) { s.Base.Source.SHA256 = strings.Repeat("f", 64) },
	} {
		bad := scope
		alter(&bad)
		if _, err := CompilePerspectiveLinesPlan(bad, raw, icc); err == nil {
			t.Fatal("invalid source admitted")
		}
	}
	if _, err := CompilePerspectiveLinesPlan(scope, raw, strings.Repeat("d", 64)); err == nil {
		t.Fatal("foreign ICC admitted")
	}
	spaced := append(bytes.Clone(raw), ' ')
	changed := scope
	changed.Base.Source.SHA256 = renderDerivativeBytesSHA(spaced)
	if _, err := CompilePerspectiveLinesPlan(changed, spaced, icc); err == nil {
		t.Fatal("noncanonical source admitted")
	}
}
func TestPerspectiveLinesPreparationRejectsMutationAndAliases(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, true)
	for _, alter := range []func(*PerspectiveLinesPlan){
		func(p *PerspectiveLinesPlan) { p.task = uuid.New() },
		func(p *PerspectiveLinesPlan) { p.params = []byte(`{}`) },
		func(p *PerspectiveLinesPlan) { p.profile = []byte(`{}`) },
		func(p *PerspectiveLinesPlan) { p.inputs[0], p.inputs[1] = p.inputs[1], p.inputs[0] },
		func(p *PerspectiveLinesPlan) { p.inputs = p.inputs[:2] },
		func(p *PerspectiveLinesPlan) { p.scope.Project = uuid.New() },
	} {
		plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
		if err != nil {
			t.Fatal(err)
		}
		params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
		alter(&plan)
		if _, err := PreparePerspectiveLinesTask(plan, params); err == nil {
			t.Fatal("mutated plan admitted")
		}
	}
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range plan.Inputs() {
		params := review.ArtifactRef{ID: ref.ID, SHA256: renderDerivativeBytesSHA(plan.Params())}
		if _, err := PreparePerspectiveLinesTask(plan, params); err == nil {
			t.Fatal("Params alias admitted")
		}
	}
}

func TestPerspectiveLinesClosureEntriesAreSortedOwnedAndTyped(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, true)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	entries, err := plan.Entries(params)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i, entry := range entries {
		counts[entry.Contract]++
		if i > 0 && bytes.Compare(entries[i-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0 {
			t.Fatal("unsorted or duplicate closure")
		}
	}
	if counts["PerspectiveLineDetectionParams"] != 1 || counts["WorkingImage"] != 1 || counts["NpyArray"] != 2 {
		t.Fatal(counts)
	}
	entries[0].Contract = "forged"
	again, err := plan.Entries(params)
	if err != nil || again[0].Contract == "forged" {
		t.Fatal("closure mutable", err)
	}
	params.SHA256 = strings.Repeat("f", 64)
	if _, err = plan.Entries(params); err == nil {
		t.Fatal("foreign Params accepted")
	}
}
