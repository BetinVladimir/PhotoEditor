package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func finalGenerationDeploymentFixture() AuthoredFinalDeployment {
	tenant := uuid.New()
	return AuthoredFinalDeployment{SchemaVersion: 3, Tenants: []uuid.UUID{tenant}, NativeReplayPolicyPath: "/operator/replays.json", NativeReplayPolicySHA: strings.Repeat("a", 64), Generations: []AuthoredFinalTenantGeneration{{Tenant: tenant, Evidence: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}}}}
}
func TestFinal338DeploymentPinsEveryActualTenantGeneration(t *testing.T) {
	d := finalGenerationDeploymentFixture()
	if d.validate() != nil {
		t.Fatal("valid source config")
	}
	for _, name := range []string{"missing", "foreign-tenant", "duplicate", "nil-artifact", "digest", "old-calibration", "no-native-policy"} {
		t.Run(name, func(t *testing.T) {
			d := finalGenerationDeploymentFixture()
			switch name {
			case "missing":
				d.Generations = nil
			case "foreign-tenant":
				d.Generations[0].Tenant = uuid.New()
			case "duplicate":
				d.Generations = append(d.Generations, d.Generations[0])
			case "nil-artifact":
				d.Generations[0].Evidence.ArtifactID = uuid.Nil
			case "digest":
				d.Generations[0].Evidence.SHA256 = "bad"
			case "old-calibration":
				d.CalibrationPath = "/old/catalog"
			case "no-native-policy":
				d.NativeReplayPolicyPath = ""
			}
			if d.validate() == nil {
				t.Fatal("accepted", name)
			}
		})
	}
}
func TestFinal338DeploymentPreservesHistoricalWireAndRefusesGenerationMix(t *testing.T) {
	d := AuthoredFinalDeployment{SchemaVersion: 1, Tenants: []uuid.UUID{uuid.New()}, CalibrationPath: "/old/calibration", CalibrationSHA: strings.Repeat("c", 64)}
	if d.validate() != nil {
		t.Fatal("historical rejected")
	}
	raw, e := json.Marshal(d)
	if e != nil || bytes.Contains(raw, []byte("generations")) {
		t.Fatal("historical wire changed", e)
	}
	d.Generations = finalGenerationDeploymentFixture().Generations
	if d.validate() == nil {
		t.Fatal("old generation mixed")
	}
	d.Generations = nil
	d.SchemaVersion = 2
	d.NativeReplayPolicyPath = "/operator/replays.json"
	d.NativeReplayPolicySHA = strings.Repeat("d", 64)
	if d.validate() != nil {
		t.Fatal("version2 rejected")
	}
}
