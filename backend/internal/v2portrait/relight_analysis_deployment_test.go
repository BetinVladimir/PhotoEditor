package v2portrait

import (
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestAnalysisDeploymentRejectsCrossScopeAndUntrustedPolicy(t *testing.T) {
	request, _, expected := analysisQualificationFixture(t)
	entry := analysisDeploymentEntry{Tenant: request.Tenant, Project: request.Project, Operation: request.Operation, Runtime: request.Runtime, DatasetSHA: expected.DatasetSHA, Issuer: expected.IssuerID, Checks: []string{"quality"}, Fixtures: []string{strings.Repeat("e", 64)}, Evidence: []string{strings.Repeat("f", 64)}, Calibrations: []string{}, LicenseCleared: true, Capacity: request.Capacity}
	for _, fault := range []string{"none", "duplicate", "license", "fixture_overlap", "bad_hash", "no_checks", "operation", "metal_budget", "extra"} {
		changed := entry
		wire := analysisDeploymentWire{SchemaVersion: 1}
		switch fault {
		case "license":
			changed.LicenseCleared = false
		case "fixture_overlap":
			changed.Calibrations = append([]string(nil), changed.Fixtures...)
		case "bad_hash":
			changed.DatasetSHA = "invalid"
		case "no_checks":
			changed.Checks = nil
		case "operation":
			changed.Operation = "image.relight"
		case "metal_budget":
			changed.Runtime.Backend = "mps"
			changed.Capacity.VRAMBytes = 10000000001
		}
		wire.Projects = []analysisDeploymentEntry{changed}
		if fault == "duplicate" {
			wire.Projects = append(wire.Projects, changed)
		}
		raw, err := canonical.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if fault == "extra" {
			var fields map[string]any
			json.Unmarshal(raw, &fields)
			fields["extra"] = true
			raw, err = canonical.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err = DecodeRelightAnalysisDeployment(raw)
		if (err == nil) != (fault == "none") {
			t.Fatal(fault, err)
		}
	}
}
