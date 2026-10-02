package v2learning

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
)

func TestActiveBaselineRejectsForeignTrainingAndZeroProfile(t *testing.T) {
	id := uuid.New()
	trainingSHA := strings.Repeat("a", 64)
	profile := v2draft.CandidateProfile{SchemaVersion: 1, ProfileID: id,
		TrainingSHA: trainingSHA, FeatureOrder: "scene12_v1", RidgeLambda: .1}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	body, err := canonical.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verifyActiveBaselineBytes(body, id, trainingSHA); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyActiveBaselineBytes(body, uuid.New(), trainingSHA); err == nil {
		t.Fatal("accepted baseline from another profile")
	}
	if _, err = verifyActiveBaselineBytes(body, id, strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted baseline from another training manifest")
	}
	profile.TrainingSHA = strings.Repeat("0", 64)
	body, err = canonical.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = verifyActiveBaselineBytes(body, id, profile.TrainingSHA); err == nil {
		t.Fatal("accepted zero profile as an active trained model")
	}
}
