package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
	"strings"
	"testing"
)

// Generic derivative fixture exercises shared exact task binding only, not
// positive multi-source SQL authority or successful graph activation.
func TestMultiPreviewMaterializationTaskSubstitutions(t *testing.T) {
	plan, err := CompilePerspectivePreviewPlan(perspectivePlanBundle(t), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	docs := plan.Documents()
	tasks := make([]PreparedTask, 2)
	for i, d := range docs {
		tasks[i], err = PrepareRenderDerivativeTask(d, review.ArtifactRef{ID: uuid.New(), SHA256: d.ParamsSHA})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := renderDerivativeTaskBodies(docs, tasks); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"operation", "version", "class", "timeout", "inputs", "node", "params_id"} {
		t.Run(mode, func(t *testing.T) {
			bad := append([]PreparedTask(nil), tasks...)
			switch mode {
			case "operation":
				bad[0].Operation = "media.export"
			case "version":
				bad[0].Version = "2"
			case "class":
				bad[0].ResourceClass = "gpu"
			case "timeout":
				bad[0].TimeoutSeconds = 301
			case "inputs":
				bad[0].Inputs = append([]review.ArtifactRef(nil), bad[0].Inputs...)
				bad[0].Inputs[0].ID = uuid.New()
			case "node":
				bad[0].NodeKey = "render.derivative:preview2560"
			case "params_id":
				bad[0].Params.ID = uuid.Nil
			}
			if _, err := renderDerivativeTaskBodies(docs, bad); err == nil {
				t.Fatal("substitution accepted")
			}
		})
	}
	if _, err := renderDerivativeTaskBodies(docs, tasks[:1]); err == nil {
		t.Fatal("partial pair accepted")
	}
}
func TestMultiPreviewMaterializationPreflight(t *testing.T) {
	if err := MaterializeMultiPreviewTasks(context.Background(), nil, MultiPreviewPlan{}, v2render.MultiResultContent{}, nil); err == nil {
		t.Fatal("unconfigured materialization")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MaterializeMultiPreviewTasks(ctx, nil, MultiPreviewPlan{}, v2render.MultiResultContent{}, nil); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
