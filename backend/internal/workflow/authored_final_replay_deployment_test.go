package workflow

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestAuthoredFinalReplayDeploymentRequiresExplicitVersionedPolicy(t *testing.T) {
	base := AuthoredFinalDeployment{SchemaVersion: 1, Tenants: []uuid.UUID{uuid.New()}, CalibrationPath: "/deployment/final.json", CalibrationSHA: strings.Repeat("a", 64)}
	if base.validate() != nil {
		t.Fatal("ordinary v1 CPU deployment changed")
	}
	mixed := base
	mixed.NativeReplayPolicyPath = "/deployment/replay.json"
	mixed.NativeReplayPolicySHA = strings.Repeat("b", 64)
	if mixed.validate() == nil {
		t.Fatal("v1 silently enabled new replay policy")
	}
	mixed.SchemaVersion = 2
	if mixed.validate() != nil {
		t.Fatal("explicit version2 policy refused")
	}
}
func TestAuthoredFinalReplayDeploymentRejectsIncompleteOrRelativePolicy(t *testing.T) {
	for _, mutation := range []string{"absent", "relative", "missingpin", "badpin", "version"} {
		t.Run(mutation, func(t *testing.T) {
			d := AuthoredFinalDeployment{SchemaVersion: 2, Tenants: []uuid.UUID{uuid.New()}, CalibrationPath: "/deployment/final.json", CalibrationSHA: strings.Repeat("a", 64), NativeReplayPolicyPath: "/deployment/replay.json", NativeReplayPolicySHA: strings.Repeat("b", 64)}
			switch mutation {
			case "absent":
				d.NativeReplayPolicyPath = ""
				d.NativeReplayPolicySHA = ""
			case "relative":
				d.NativeReplayPolicyPath = "replay.json"
			case "missingpin":
				d.NativeReplayPolicySHA = ""
			case "badpin":
				d.NativeReplayPolicySHA = "none"
			case "version":
				d.SchemaVersion = 3
			}
			if d.validate() == nil {
				t.Fatal("unconfigured policy accepted")
			}
		})
	}
}
