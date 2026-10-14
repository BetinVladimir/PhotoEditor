package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPortraitDetailHoldoutRefStableAndScoped(t *testing.T) {
	run, event := uuid.New(), uuid.New()
	sha := strings.Repeat("a", 64)
	first, err := PortraitDetailHoldoutRef(run, event, sha)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PortraitDetailHoldoutRef(run, event, sha)
	if err != nil || first != second {
		t.Fatalf("unstable detail ref: %v %v %v", first, second, err)
	}
	other, err := PortraitDetailHoldoutRef(run, uuid.New(), sha)
	if err != nil || other.ArtifactID == first.ArtifactID {
		t.Fatalf("event scope lost: %v %v %v", first, other, err)
	}
	other, err = PortraitDetailHoldoutRef(uuid.New(), event, sha)
	if err != nil || other.ArtifactID == first.ArtifactID {
		t.Fatalf("run scope lost: %v %v %v", first, other, err)
	}
	for _, input := range []struct {
		run, event uuid.UUID
		sha        string
	}{{uuid.Nil, event, sha}, {run, uuid.Nil, sha}, {run, event, "bad"}} {
		if _, err := PortraitDetailHoldoutRef(input.run, input.event, input.sha); err == nil {
			t.Fatal("invalid detail identity accepted")
		}
	}
}
