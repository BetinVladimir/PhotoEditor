package workflow

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"testing"
)

func TestPerspectiveLinesMaterializationBindsWholeTask(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, true)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	task, err := PreparePerspectiveLinesTask(plan, ref)
	if err != nil {
		t.Fatal(err)
	}
	body, err := perspectiveLinesTaskBody(plan, task)
	if err != nil {
		t.Fatal(err)
	}
	exact, err := canonical.Marshal(task)
	if err != nil || !bytes.Equal(body, exact) {
		t.Fatal("changed task", err)
	}
	for _, alter := range []func(*PreparedTask){
		func(t *PreparedTask) { t.ID = uuid.New() },
		func(t *PreparedTask) { t.NodeKey = "other" },
		func(t *PreparedTask) { t.Operation = "optical.perspective" },
		func(t *PreparedTask) { t.Version = "2" },
		func(t *PreparedTask) { t.ResourceClass = "gpu" },
		func(t *PreparedTask) { t.TimeoutSeconds++ },
		func(t *PreparedTask) { t.InputDigest = "bad" },
		func(t *PreparedTask) { t.Inputs = t.Inputs[:1] },
		func(t *PreparedTask) { t.ExecutionProfile = []byte(`{}`) },
	} {
		bad := task
		bad.Inputs = plan.Inputs()
		alter(&bad)
		if _, err := perspectiveLinesTaskBody(plan, bad); err == nil {
			t.Fatal("mutated task accepted")
		}
	}
}
func TestPerspectiveLinesMaterializationCancellationBeforeDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MaterializePerspectiveLinesTask(ctx, nil, PerspectiveLinesPlan{}, PreparedTask{}); err != context.Canceled {
		t.Fatal(err)
	}
	if err := MaterializePerspectiveLinesTask(context.Background(), nil, PerspectiveLinesPlan{}, PreparedTask{}); err == nil {
		t.Fatal("uncompiled plan accepted")
	}
}
