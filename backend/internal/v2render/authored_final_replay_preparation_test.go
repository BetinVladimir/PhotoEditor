package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

// These are policy-selection unit boundaries, not qualification/install evidence.
func TestAuthoredFinalReplayTrustedSelectionOwnsExpected(t *testing.T) {
	expected := v2qualification.Expected{Operation: "image.upscale", Version: "1", Backend: "mps", ModelLockSHA: strings.Repeat("a", 64), LicenseCleared: true, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("b", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("c", 64): {}}}
	report := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}
	s := &SQLAuthoredFinalReplayPreparation{policies: []authoredFinalTrustedReplayPolicy{{expected, report}}}
	request := v2input.AuthoredNativeReplayRequest{Original: v2input.AuthoredNativeObservation{Task: v2input.AuthoredNativeProducer{Operation: "image.upscale", Version: "1", ProfileJCS: `{"backend":"mps","model_lock_id":"` + strings.Repeat("a", 64) + `"}`}}}
	got, ref, err := s.selectPolicy(request)
	if err != nil || ref != report {
		t.Fatal("trusted selection failed", err)
	}
	got.RequiredChecks[0] = "mutated"
	delete(got.TestFixtureSHAs, strings.Repeat("b", 64))
	got.EvidenceSHAs["foreign"] = struct{}{}
	again, _, err := s.selectPolicy(request)
	if err != nil || again.RequiredChecks[0] != "quality" || len(again.TestFixtureSHAs) != 1 || len(again.EvidenceSHAs) != 1 {
		t.Fatal("caller mutated owned trusted policy")
	}
	for _, name := range []string{"backend", "model", "operation", "version", "unconfigured"} {
		t.Run(name, func(t *testing.T) {
			r := request
			owner := s
			switch name {
			case "backend":
				r.Original.Task.ProfileJCS = strings.Replace(r.Original.Task.ProfileJCS, "mps", "cuda", 1)
			case "model":
				r.Original.Task.ProfileJCS = strings.Replace(r.Original.Task.ProfileJCS, strings.Repeat("a", 64), strings.Repeat("f", 64), 1)
			case "operation":
				r.Original.Task.Operation = "other"
			case "version":
				r.Original.Task.Version = "2"
			case "unconfigured":
				owner = &SQLAuthoredFinalReplayPreparation{}
			}
			if _, _, e := owner.selectPolicy(r); e == nil {
				t.Fatal("foreign/default policy accepted")
			}
		})
	}
}
func TestAuthoredFinalReplayMissingProviderCannotQualify(t *testing.T) {
	var s *SQLAuthoredFinalReplayPreparation
	_, ready, err := s.PrepareRequiredFinalReplays(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), 2, uuid.New())
	if err == nil || ready {
		t.Fatal("missing provider became ready")
	}
}
