package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPerspectiveApplyDecisionStrictWire(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	good := PerspectiveApplyDecision{SchemaVersion: 1, ResultID: uuid.New(), Root: ref, QualityRun: uuid.New(), QualityResult: uuid.New(), QualityReport: ref, ConfirmGeometryReset: true}
	raw, err := canonical.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePerspectiveApplyDecision(raw); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PerspectiveApplyDecision){func(v *PerspectiveApplyDecision) { v.SchemaVersion = 2 }, func(v *PerspectiveApplyDecision) { v.ConfirmGeometryReset = false }, func(v *PerspectiveApplyDecision) { v.ResultID = uuid.Nil }, func(v *PerspectiveApplyDecision) { v.QualityRun = uuid.Nil }, func(v *PerspectiveApplyDecision) { v.Root.SHA256 = "invalid" }} {
		bad := good
		change(&bad)
		raw, err := canonical.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodePerspectiveApplyDecision(raw); err == nil {
			t.Fatal("invalid decision admitted")
		}
	}
	if _, err := DecodePerspectiveApplyDecision([]byte(`{"schema_version":1,"schema_version":2}`)); err == nil {
		t.Fatal("duplicate admitted")
	}
}
func TestPerspectiveApplyCancellationAndUnavailable(t *testing.T) {
	s := PerspectiveProposalService{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Apply(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), uuid.New(), PerspectiveApplyDecision{}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := s.Apply(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), uuid.New(), PerspectiveApplyDecision{}); err == nil {
		t.Fatal("unavailable service admitted")
	}
}
