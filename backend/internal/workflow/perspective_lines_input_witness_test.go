package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestPerspectiveLinesAdmissionWitnessBindsOriginalLocators(t *testing.T) {
	f := newLinesInputsFixture(t, true)
	verified, err := f.verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := verified.AdmissionJSON(f.plan, "photos")
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Task   uuid.UUID                     `json:"task_id"`
		Source perspectiveLinesObjectWitness `json:"source"`
		Arrays []struct {
			Role   string                        `json:"role"`
			Object perspectiveLinesObjectWitness `json:"object"`
		} `json:"arrays"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Task != f.plan.Task() || value.Source.Artifact != f.plan.scope.Base.Source.ArtifactID || value.Source.Version != f.source.VersionID || len(value.Arrays) != 2 || value.Arrays[0].Role != "WorkingPixels" || value.Arrays[1].Role != "WorkingAlpha" || value.Source.Bucket != "photos" {
		t.Fatal("lost witness binding")
	}
	if value.Arrays[0].Object.Version != verified.Arrays()[0].Object.VersionID {
		t.Fatal("array locator changed")
	}
}
func TestPerspectiveLinesAdmissionWitnessRejectsMissingForeignOrChangedEvidence(t *testing.T) {
	f := newLinesInputsFixture(t, true)
	verified, err := f.verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*VerifiedPerspectiveLinesInputs){
		func(v *VerifiedPerspectiveLinesInputs) { v.task = uuid.New() },
		func(v *VerifiedPerspectiveLinesInputs) { v.scope.Project = uuid.New() },
		func(v *VerifiedPerspectiveLinesInputs) { v.snapshot = uuid.Nil },
		func(v *VerifiedPerspectiveLinesInputs) { v.who.UserID = uuid.Nil },
		func(v *VerifiedPerspectiveLinesInputs) { v.source.Bytes++ },
		func(v *VerifiedPerspectiveLinesInputs) { v.arrays = v.arrays[:1] },
		func(v *VerifiedPerspectiveLinesInputs) { v.arrays[0], v.arrays[1] = v.arrays[1], v.arrays[0] },
		func(v *VerifiedPerspectiveLinesInputs) { v.arrays[0].Object.SHA256 = "bad" },
	} {
		bad := verified
		bad.arrays = verified.Arrays()
		alter(&bad)
		if _, err := bad.AdmissionJSON(f.plan, "photos"); err == nil {
			t.Fatal("invalid witness accepted")
		}
	}
	if _, err := (VerifiedPerspectiveLinesInputs{}).AdmissionJSON(f.plan, "photos"); err == nil {
		t.Fatal("unverified inputs accepted")
	}
}
