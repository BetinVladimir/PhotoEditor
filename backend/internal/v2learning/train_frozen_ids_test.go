package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTrainingFrozenRefsAreDeterministicAndSideSeparated(t *testing.T) {
	run, event := uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	base, err := TrainingFrozenRef(run, event, "base", sha)
	if err != nil {
		t.Fatal(err)
	}
	again, err := TrainingFrozenRef(run, event, "base", sha)
	if err != nil || again != base {
		t.Fatal("frozen ref changed across retries")
	}
	approved, err := TrainingFrozenRef(run, event, "approved", sha)
	if err != nil || approved.ArtifactID == base.ArtifactID {
		t.Fatal("base and approved artifacts share identity")
	}
	if _, err := TrainingFrozenRef(run, event, "other", sha); err == nil {
		t.Fatal("unknown frozen side accepted")
	}
}
