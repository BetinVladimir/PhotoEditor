package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestRelightDeploymentOwnsStrictProjectConfiguration(t *testing.T) {
	_, pin, _, _, expected := relightQualificationFixture(t)
	fixtures := []string{}
	for sha := range expected.TestFixtureSHAs {
		fixtures = append(fixtures, sha)
	}
	evidence := []string{}
	for sha := range expected.EvidenceSHAs {
		evidence = append(evidence, sha)
	}
	policy := relightProjectWire{Tenant: uuid.New(), Project: uuid.New(), Pin: pin, Checks: expected.RequiredChecks, Fixtures: fixtures, Evidence: evidence, Calibrations: []string{}, LicenseCleared: true, Capacity: LocalResourceEstimate{PixelCount: 100, RAMBytes: 1000, ScratchBytes: 1000, OutputBytes: 1000}, Raster: relightLimitWire{10, 20, 30}, Batch: relightLimitWire{20, 40, 60}}
	wire := relightDeploymentWire{SchemaVersion: 1, Projects: []relightProjectWire{policy}}
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := DecodeRelightDeployment(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'x'
	if len(deployment.projects) != 1 || deployment.projects[0].Project != policy.Project {
		t.Fatal("borrowed configuration")
	}
	for _, fault := range []string{"duplicate_project", "license", "checks", "digest", "capacity", "limits"} {
		changed := policy
		switch fault {
		case "license":
			changed.LicenseCleared = false
		case "checks":
			changed.Checks = []string{"same", "same"}
		case "digest":
			changed.Evidence = []string{"invalid"}
		case "capacity":
			changed.Capacity.VRAMBytes = 1
		case "limits":
			changed.Batch.PointChecks = 1
		}
		projects := []relightProjectWire{changed}
		if fault == "duplicate_project" {
			projects = append(projects, changed)
		}
		bad, _ := canonical.Marshal(relightDeploymentWire{SchemaVersion: 1, Projects: projects})
		if _, err := DecodeRelightDeployment(bad); err == nil {
			t.Fatal("accepted", fault)
		}
	}
}

func TestRelightProjectRouterRefusesUnknownScopeWithoutDatabase(t *testing.T) {
	router := &RelightProjectServices{services: map[[2]uuid.UUID]*RelightSubmissionService{}}
	if raw, err := router.SubmitProjectKeyed(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), "key", RelightDraftIntent{}); err == nil || raw != nil {
		t.Fatal("unknown scope accepted")
	}
	service := &RelightSubmissionService{tenant: uuid.New(), project: uuid.New()}
	if _, err := service.SubmitProjectKeyed(context.Background(), auth.Identity{}, uuid.New(), service.project, "key", RelightDraftIntent{}); err == nil {
		t.Fatal("wrong tenant accepted")
	}
}
