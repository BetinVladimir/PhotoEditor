package v2input

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestFrozenSessionPlanClosureIsCompleteAndRepeatable(t *testing.T) {
	calibration := Ref{ArtifactID: uuid.New(),
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	policy := frozenSessionPolicy{SchemaVersion: 1, TimeGapSeconds: 60,
		NeighborCount: 4, ProtectManualPicks: true,
		CameraOffsets: []frozenCameraOffset{}, Calibration: calibration}
	raw, err := canonical.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(policy)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseFrozenPolicy(raw, sha)
	if err != nil || parsed.TimeGapSeconds != 60 {
		t.Fatalf("policy invalid: %v", err)
	}
	if _, err := parseFrozenPolicy(raw, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err == nil {
		t.Fatal("accepted mismatched policy hash")
	}
	workflow := uuid.New()
	descriptors, _ := CompileSessionDescriptors(workflow, 129)
	results := []SessionShardResult{
		{TaskID: descriptors.Tasks[0].ID, Document: Ref{ArtifactID: uuid.New(), SHA256: sha}},
		{TaskID: descriptors.Tasks[1].ID, Document: Ref{ArtifactID: uuid.New(), SHA256: sha}},
	}
	params := Ref{ArtifactID: uuid.New(), SHA256: sha}
	frozen := FrozenSessionPlan{ItemCount: 129, Policy: parsed,
		Context:        Ref{ArtifactID: uuid.New(), SHA256: sha},
		SnapshotDigest: sha, PolicySHA: sha}
	closure, err := CompileSessionPlanClosure(workflow, frozen, results, params)
	if err != nil || closure.Open == nil || !digestPattern.MatchString(closure.InputDigest) {
		t.Fatalf("closure invalid: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		cursor, err := closure.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		contracts := map[string]int{}
		for {
			entry, ok, err := cursor.Next(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			contracts[entry.Contract]++
		}
		if contracts["Params"] != 1 || contracts["Calibration"] != 1 ||
			contracts["SessionPlanContext"] != 1 || contracts["DescriptorShard"] != 2 {
			t.Fatalf("incomplete closure: %+v", contracts)
		}
	}
	results[1].Document = results[0].Document
	if _, err := CompileSessionPlanClosure(workflow, frozen, results, params); err == nil {
		t.Fatal("accepted duplicate descriptor")
	}
}
