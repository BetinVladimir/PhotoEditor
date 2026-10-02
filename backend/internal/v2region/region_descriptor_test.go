package v2region

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRegionDescriptorProjectsVerifiedEvidenceWithoutObjectLocations(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	descriptor, err := plan.Descriptor(regionSHA)
	if err != nil {
		t.Fatal(err)
	}
	shape, err := plan.SourceShape()
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.ParamsSHA256 != plan.SHA256() || descriptor.Canvas.Width != shape.Width || descriptor.Canvas.Height != shape.Height || descriptor.Alpha != shape.HasAlpha || descriptor.ICCSHA256 != regionSHA {
		t.Fatal("descriptor changed verified evidence")
	}
	raw, _ := json.Marshal(descriptor)
	for _, private := range []string{"object_key", "object_version", "bucket", "frozen_recipe", "inputs"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private transport leaked", private)
		}
	}
	descriptor.Request.MaxEdge = 64
	again, err := plan.Descriptor(regionSHA)
	if err != nil || again.Request.MaxEdge == descriptor.Request.MaxEdge {
		t.Fatal("projection mutated frozen plan")
	}
}
func TestRegionDescriptorRejectsCorruptPlanAndInvalidOutputPolicy(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	for _, icc := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if _, err := plan.Descriptor(icc); err == nil {
			t.Fatal("invalid ICC accepted")
		}
	}
	corrupt := plan
	corrupt.body = append([]byte(nil), plan.body...)
	corrupt.body[0] = ' '
	if _, err := corrupt.Descriptor(regionSHA); err == nil {
		t.Fatal("corrupt plan accepted")
	}
	inconsistent := plan
	inconsistent.canvas.Width++
	if _, err := inconsistent.Descriptor(regionSHA); err == nil {
		t.Fatal("unbound canvas accepted")
	}
}
