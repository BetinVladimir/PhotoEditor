package workflow

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
	"testing"
)

func TestMultiQualityRequiresVerifiedContent(t *testing.T) {
	if _, err := CompileMultiQualityPlan(v2render.MultiPreviewSource{}, v2render.MultiResultContent{}); err == nil {
		t.Fatal("unverified source compiled")
	}
	if _, err := PrepareMultiQualityTask(MultiQualityPlan{}, review.ArtifactRef{ID: uuid.New(), SHA256: "a"}); err == nil {
		t.Fatal("unverified plan admitted")
	}
}
