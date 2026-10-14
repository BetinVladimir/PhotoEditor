package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestPerspectivePreviewTaskBodiesBindExactPublishedTasks(t *testing.T) {
	bundle := perspectivePlanBundle(t)
	plan, err := CompilePerspectivePreviewPlan(bundle, strings.Repeat("e", 64))
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
	if bodies, err := perspectivePreviewTaskBodies(docs, tasks); err != nil || len(bodies) != 2 {
		t.Fatal("valid tasks refused", err)
	}
	for _, mode := range []string{"swap", "id", "profile", "digest", "params"} {
		t.Run(mode, func(t *testing.T) {
			bad := append([]PreparedTask(nil), tasks...)
			switch mode {
			case "swap":
				bad[0], bad[1] = bad[1], bad[0]
			case "id":
				bad[0].ID = uuid.New()
			case "profile":
				bad[0].ExecutionProfile = []byte(`{}`)
			case "digest":
				bad[0].InputDigest = strings.Repeat("a", 64)
			case "params":
				bad[0].Params.SHA256 = strings.Repeat("b", 64)
			}
			if _, err := perspectivePreviewTaskBodies(docs, bad); err == nil {
				t.Fatal("task substitution accepted")
			}
		})
	}
	if err := MaterializePerspectivePreviewTasks(context.Background(), nil, plan, bundle, tasks); err == nil {
		t.Fatal("missing DB accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MaterializePerspectivePreviewTasks(ctx, nil, plan, bundle, tasks); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
