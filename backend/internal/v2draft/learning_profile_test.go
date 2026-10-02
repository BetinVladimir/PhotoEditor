package v2draft

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestCandidateProfileRequiresExactTrainingProvenance(t *testing.T) {
	id := uuid.New()
	sha := strings.Repeat("a", 64)
	profile := CandidateProfile{SchemaVersion: 1, ProfileID: id,
		TrainingSHA: sha, FeatureOrder: "scene12_v1", RidgeLambda: .1}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	body, err := canonical.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := candidateFromModel(body, id, sha); err != nil || got != profile {
		t.Fatal("valid model provenance", got, err)
	}
	for _, mismatch := range []struct {
		id  uuid.UUID
		sha string
	}{{uuid.New(), sha}, {id, strings.Repeat("b", 64)}, {id, zeroTrainingSHA}} {
		if _, err := candidateFromModel(body, mismatch.id, mismatch.sha); err == nil {
			t.Fatal("accepted mismatched provenance", mismatch)
		}
	}
	profile.TrainingSHA = zeroTrainingSHA
	builtin, err := canonical.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := candidateFromModel(builtin, id, zeroTrainingSHA); err == nil {
		t.Fatal("accepted builtin marker as a trained version")
	}
}
