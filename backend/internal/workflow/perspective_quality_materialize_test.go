package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestPerspectiveQualityTaskBodyRejectsDispatchSubstitution(t *testing.T) {
	plan, err := CompilePerspectiveQualityPlan(perspectivePlanBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	mutations := []struct {
		name   string
		change func(*PreparedTask)
	}{
		{"identity", func(p *PreparedTask) { p.ID = uuid.New() }},
		{"node", func(p *PreparedTask) { p.NodeKey = "quality.check:preview" }},
		{"operation", func(p *PreparedTask) { p.Operation = "render.derivative" }},
		{"version", func(p *PreparedTask) { p.Version = "2" }},
		{"pool", func(p *PreparedTask) { p.ResourceClass = "gpu" }},
		{"timeout", func(p *PreparedTask) { p.TimeoutSeconds = 300 }},
		{"digest", func(p *PreparedTask) { p.InputDigest = "changed" }},
		{"profile", func(p *PreparedTask) { p.ExecutionProfile = []byte(`{}`) }},
		{"inputs", func(p *PreparedTask) { p.Inputs[1].ID = uuid.New() }},
		{"params", func(p *PreparedTask) { p.Params.SHA256 = "changed" }},
	}
	task, err := PreparePerspectiveQualityTask(plan, params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := perspectiveQualityTaskBody(plan, task); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate, err := PreparePerspectiveQualityTask(plan, params)
			if err != nil {
				t.Fatal(err)
			}
			mutation.change(&candidate)
			if _, err := perspectiveQualityTaskBody(plan, candidate); err == nil {
				t.Fatal("substituted task admitted")
			}
		})
	}
}

func TestPerspectiveQualityMaterializationAndSealFailBeforeIO(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	task, err := PreparePerspectiveQualityTask(plan, params)
	if err != nil {
		t.Fatal(err)
	}
	if err := MaterializePerspectiveQualityTask(context.Background(), nil, plan, bundle, task); err == nil {
		t.Fatal("missing DB admitted")
	}
	if err := SealPerspectiveQuality(context.Background(), nil, plan, bundle, v2render.VerifiedPerspectivePreviewInputs{}); err == nil {
		t.Fatal("missing proof admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MaterializePerspectiveQualityTask(ctx, nil, plan, bundle, task); err != context.Canceled {
		t.Fatal(err)
	}
	if err := SealPerspectiveQuality(ctx, nil, plan, bundle, v2render.VerifiedPerspectivePreviewInputs{}); err != context.Canceled {
		t.Fatal(err)
	}
}
