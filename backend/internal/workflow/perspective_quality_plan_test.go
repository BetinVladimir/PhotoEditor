package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestPerspectiveQualityPlanOwnsGeometryTargetAndResources(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Run() == bundle.Source().Run {
		t.Fatal("quality mutated parent run")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(plan.Params(), &body) != nil || string(body["baseline"]) != "null" || string(body["protected"]) != "[]" || string(body["seams"]) != "[]" {
		t.Fatal("baseline/mask invented")
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	task, err := PreparePerspectiveQualityTask(plan, params)
	if err != nil {
		t.Fatal(err)
	}
	if task.Operation != "quality.check" || task.ResourceClass != "cpu" || task.TimeoutSeconds != 600 {
		t.Fatal(task)
	}
	if task.Inputs[0].ID != bundle.Result().WorkingImage.ArtifactID {
		t.Fatal("target source substitution")
	}
	estimate, err := ParseResourceEstimate(task.ExecutionProfile)
	if err != nil || estimate.VRAMBytes != 0 || estimate.PixelCount != plan.pixels {
		t.Fatal(estimate, err)
	}
	expectedParams := bytes.Clone(plan.params)
	raw := plan.Params()
	raw[0] = 'x'
	inputs := plan.Inputs()
	inputs[0].ID = uuid.New()
	if !bytes.Equal(plan.Params(), expectedParams) || plan.Inputs()[0].ID != task.Inputs[0].ID {
		t.Fatal("mutable plan exposure")
	}
}
func TestPerspectiveQualityPlanRejectsUnownedBundleAndTaskMutation(t *testing.T) {
	if _, err := CompilePerspectiveQualityPlan(v2render.PerspectivePreviewBundle{}); err == nil {
		t.Fatal("empty bundle")
	}
	good, err := CompilePerspectiveQualityPlan(perspectivePlanBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(good.Params())}
	for _, alter := range []func(*PerspectiveQualityPlan){
		func(p *PerspectiveQualityPlan) { p.run = uuid.New() },
		func(p *PerspectiveQualityPlan) { p.task = uuid.New() },
		func(p *PerspectiveQualityPlan) { p.pixels++ },
		func(p *PerspectiveQualityPlan) { p.profile = []byte(`{"backend":"cuda"}`) },
		func(p *PerspectiveQualityPlan) { p.params = []byte(`{}`) },
	} {
		bad := good
		alter(&bad)
		if _, err := PreparePerspectiveQualityTask(bad, params); err == nil {
			t.Fatal("mutated plan admitted")
		}
	}
	params.ID = good.Inputs()[0].ID
	if _, err := PreparePerspectiveQualityTask(good, params); err == nil {
		t.Fatal("Params aliases target")
	}
}
