package v2optical

import (
	"encoding/json"
	"testing"
)

func TestMultiResourcesReserveFullStackAndCylindricalFields(t *testing.T) {
	p, originals := multiOriginals(t)
	body, _, _ := p.Compile()
	planar, err := MultiResources("optical.panorama", body, originals)
	if err != nil {
		t.Fatal(err)
	}
	if planar.VRAMBytes != 0 || planar.PixelCount != 10000 || planar.RAMBytes <= planar.OutputBytes || planar.ScratchBytes <= planar.OutputBytes {
		t.Fatal("incomplete resource reservation")
	}
	p.Projection = "cylindrical"
	body, _, _ = p.Compile()
	cylindrical, err := MultiResources("optical.panorama", body, originals)
	if err != nil {
		t.Fatal(err)
	}
	if cylindrical.RAMBytes <= planar.RAMBytes || cylindrical.ScratchBytes <= planar.ScratchBytes || cylindrical.OutputBytes <= planar.OutputBytes {
		t.Fatal("projection omitted")
	}
	if err := ValidateMultiCapacity(cylindrical, cylindrical); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ram", "scratch", "output", "vram"} {
		capacity := cylindrical
		switch field {
		case "ram":
			capacity.RAMBytes--
		case "scratch":
			capacity.ScratchBytes--
		case "output":
			capacity.OutputBytes--
		case "vram":
			capacity.VRAMBytes = 1
		}
		if ValidateMultiCapacity(cylindrical, capacity) == nil {
			t.Fatal("under-reserved capacity accepted", field)
		}
	}
}
func TestMultiProfileBindsAllEvidenceAndResources(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
		qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
		if err != nil {
			t.Fatal(err)
		}
		required, err := MultiResources(operation, params, originals)
		if err != nil {
			t.Fatal(err)
		}
		profile, err := CompileMultiExecutionProfile(operation, params, originals, qualified, required)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Runtime   string                `json:"multi_runtime_sha"`
			Resources MultiResourceEstimate `json:"resource_estimate"`
		}
		if json.Unmarshal(profile, &decoded) != nil || decoded.Runtime != pin.RuntimeLockSHA || decoded.Resources != required {
			t.Fatal("profile binding lost")
		}
		originals[0], originals[1] = originals[1], originals[0]
		if _, err := CompileMultiExecutionProfile(operation, params, originals, qualified, required); err == nil {
			t.Fatal("original order changed")
		}
	}
}
