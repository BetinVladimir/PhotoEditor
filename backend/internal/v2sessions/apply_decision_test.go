package v2sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestValidateApplyDecision(t *testing.T) {
	snapshot, group, recipe := uuid.New(), uuid.New(), uuid.New()
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const policy = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan := scenePlan{SchemaVersion: 1, SnapshotID: snapshot, SnapshotDigest: digest,
		PolicySHA: policy, Groups: []sceneGroup{{ID: group, Ordinals: []int{0, 1},
			ReferenceOrdinal: 0, Confidence: .8, Corrections: ArtifactRef{ArtifactID: recipe, SHA256: digest}}}}
	raw, err := canonical.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	req := ApplyDecision{PlanSHA: hex.EncodeToString(sum[:]), ApprovedGroups: []uuid.UUID{group},
		Recipe: ArtifactRef{ArtifactID: recipe, SHA256: digest}, ExpectedRevision: "1"}
	if _, err := validateApplyDecision(raw, req, snapshot, digest, policy, 2); err != nil {
		t.Fatal(err)
	}
	duplicate := req
	duplicate.ApprovedGroups = []uuid.UUID{group, group}
	if _, err := validateApplyDecision(raw, duplicate, snapshot, digest, policy, 2); err == nil {
		t.Fatal("duplicate approved group accepted")
	}
	unknown := req
	unknown.ApprovedGroups = []uuid.UUID{uuid.New()}
	if _, err := validateApplyDecision(raw, unknown, snapshot, digest, policy, 2); err == nil {
		t.Fatal("unknown approved group accepted")
	}
	if _, err := validateApplyDecision(raw, req, snapshot, digest, digest, 2); err == nil {
		t.Fatal("stale policy accepted")
	}
	if _, err := validateApplyDecision(raw, req, snapshot, digest, policy, 3); err == nil {
		t.Fatal("incomplete snapshot partition accepted")
	}
	if _, err := validateApplyDecision(raw, req, snapshot, digest, policy, 1); err == nil {
		t.Fatal("out-of-range snapshot ordinal accepted")
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-2] = '0'
	if _, err := validateApplyDecision(tampered, req, snapshot, digest, policy, 2); err == nil {
		t.Fatal("tampered plan accepted")
	}
	invalidPlan := plan
	invalidPlan.Groups = append([]sceneGroup(nil), plan.Groups...)
	invalidPlan.Groups[0].Ordinals = []int{0, 0}
	invalidRaw, err := canonical.Marshal(invalidPlan)
	if err != nil {
		t.Fatal(err)
	}
	invalidSHA := sha256.Sum256(invalidRaw)
	invalidReq := req
	invalidReq.PlanSHA = hex.EncodeToString(invalidSHA[:])
	if _, err := validateApplyDecision(invalidRaw, invalidReq, snapshot, digest, policy, 2); err == nil {
		t.Fatal("duplicate snapshot ordinal accepted")
	}
}

func TestProjectApplySelection(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	refA := ArtifactRef{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	refB := ArtifactRef{ArtifactID: uuid.New(), SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	plan := scenePlan{Groups: []sceneGroup{
		{ID: first, Ordinals: []int{3, 1}, Corrections: refA},
		{ID: second, Ordinals: []int{2, 0}, Corrections: refB},
	}}
	items, err := projectApplySelection(plan, []uuid.UUID{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 || items[0].Ordinal != 0 || items[0].GroupID != second ||
		items[0].Corrections != refB || items[1].Ordinal != 1 ||
		items[1].GroupID != first || items[1].Corrections != refA ||
		items[2].Ordinal != 2 || items[3].Ordinal != 3 {
		t.Fatalf("unexpected selected items: %+v", items)
	}
	items, err = projectApplySelection(plan, []uuid.UUID{first})
	if err != nil || len(items) != 2 || items[0].Ordinal != 1 || items[1].Ordinal != 3 {
		t.Fatalf("rejected group leaked into child: %+v, %v", items, err)
	}
	for _, groups := range [][]uuid.UUID{{}, {first, first}, {uuid.New()}} {
		if _, err := projectApplySelection(plan, groups); err == nil {
			t.Fatalf("invalid selection accepted: %v", groups)
		}
	}
}
