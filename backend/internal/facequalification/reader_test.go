package facequalification

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestFaceQualificationCanceledReadPreservesErrorBeforeDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (Reader{}).Verify(ctx, auth.Identity{}, uuid.New(), v2input.Ref{}, v2qualification.Expected{})
	if !errors.Is(err, context.Canceled) || result.Operation != "" {
		t.Fatalf("cancellation changed: %v", err)
	}
}
func TestFaceQualificationExpectedOwnsEveryMutablePolicyCollection(t *testing.T) {
	original := v2qualification.Expected{RequiredChecks: []string{"quality"}, CalibrationSHAs: map[string]struct{}{"cal": {}}, TestFixtureSHAs: map[string]struct{}{"fixture": {}}, EvidenceSHAs: map[string]struct{}{"evidence": {}}}
	owned := ownExpected(original)
	original.RequiredChecks[0] = "changed"
	delete(original.CalibrationSHAs, "cal")
	delete(original.TestFixtureSHAs, "fixture")
	delete(original.EvidenceSHAs, "evidence")
	if owned.RequiredChecks[0] != "quality" {
		t.Fatal("shared checks")
	}
	for name, set := range map[string]map[string]struct{}{"cal": owned.CalibrationSHAs, "fixture": owned.TestFixtureSHAs, "evidence": owned.EvidenceSHAs} {
		if _, ok := set[name]; !ok {
			t.Fatal("shared policy set", name)
		}
	}
	empty := ownExpected(v2qualification.Expected{})
	if empty.CalibrationSHAs != nil || empty.TestFixtureSHAs != nil || empty.EvidenceSHAs != nil || empty.RequiredChecks != nil {
		t.Fatal("nil semantics changed")
	}
}
func TestFaceQualificationObjectRejectsUnavailableAuthority(t *testing.T) {
	pin, err := (Reader{}).Object(context.Background(), auth.Identity{}, uuid.New(), v2qualification.Expected{Operation: "face.detect", Version: "1"}, v2input.Ref{})
	if !errors.Is(err, v2qualification.ErrUnqualified) || pin.Ref.ArtifactID != uuid.Nil {
		t.Fatalf("missing authority accepted: %v", err)
	}
}

func TestFaceQualificationMPSPolicySnapshotOwnsResourceValue(t *testing.T) {
 original := v2qualification.Expected{MPSResources: &v2qualification.MPSResourcePolicy{Metric:"mps-driver-allocated-memory-v1",AllocatorBytes:123,P95MS:4,RSSBytes:5,ScratchBytes:6}}
 owned := ownExpected(original)
 if owned.MPSResources == nil || owned.MPSResources == original.MPSResources || *owned.MPSResources != *original.MPSResources { t.Fatal("resource snapshot not independently owned") }
 original.MPSResources.AllocatorBytes = 999
 if owned.MPSResources.AllocatorBytes != 123 { t.Fatal("caller mutated retained resource policy") }
 owned.MPSResources.P95MS = 777
 if original.MPSResources.P95MS != 4 { t.Fatal("retained mutation escaped to caller") }
 if ownExpected(v2qualification.Expected{}).MPSResources != nil { t.Fatal("nil policy semantics changed") }
}
