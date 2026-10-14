package workflow

import (
	"context"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestMultiQualityMaterializationRejectsUnverifiedContent(t *testing.T) {
	if err := MaterializeMultiQualityTask(context.Background(), nil, MultiQualityPlan{}, v2render.MultiResultContent{}, PreparedTask{}); err == nil {
		t.Fatal("unverified plan admitted")
	}
}
func TestMultiQualityMaterializationCancellationPrecedesIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MaterializeMultiQualityTask(ctx, nil, MultiQualityPlan{}, v2render.MultiResultContent{}, PreparedTask{}); err != context.Canceled {
		t.Fatal(err)
	}
}
func TestMultiQualityTaskBodyRejectsUnverifiedPlan(t *testing.T) {
	if _, err := multiQualityTaskBody(MultiQualityPlan{}, PreparedTask{}); err == nil {
		t.Fatal("unverified task admitted")
	}
}
