package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func multiReportFixture(t *testing.T, operation string) (AdoptedMultiBinding, MultiImageReport, v2input.Ref) {
	t.Helper()
	binding, _, _ := multiReadFixture(t, operation)
	frames, _, _, _ := binding.Inputs()
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	refs := []v2input.Ref{{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}, {ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}}
	return binding, MultiImageReport{SchemaVersion: 1, Status: "review", Operation: operation, Reason: strings.ToUpper(strings.TrimPrefix(operation, "optical.")) + "_REVIEW;motion_fraction=0.125;REGISTRATION_INCONSISTENT", Sources: frames, Registrations: refs}, parent
}
func TestMultiReportOrderedSourcesAndMeasuredReason(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		binding, report, parent := multiReportFixture(t, operation)
		raw, err := canonical.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseMultiImageReport(raw, binding, parent)
		if err != nil || parsed.Reason != report.Reason {
			t.Fatalf("report %v", err)
		}
		parsed.Sources[0] = parsed.Sources[1]
		again, err := ParseMultiImageReport(raw, binding, parent)
		if err != nil || again.Sources[0] != report.Sources[0] {
			t.Fatal("report data aliases")
		}
	}
}
func TestMultiReportRefusesOmittedOrReorderedEvidence(t *testing.T) {
	for _, field := range []string{"order", "omitted", "duplicate_registration", "source_registration", "operation", "pass", "motion_nan", "reason", "identity", "transform"} {
		t.Run(field, func(t *testing.T) {
			binding, report, parent := multiReportFixture(t, "optical.panorama")
			switch field {
			case "order":
				report.Sources[0], report.Sources[1] = report.Sources[1], report.Sources[0]
			case "omitted":
				report.Sources = report.Sources[:1]
			case "duplicate_registration":
				report.Registrations[1] = report.Registrations[0]
			case "source_registration":
				report.Registrations[0] = report.Sources[0].Source
			case "operation":
				report.Operation = "optical.focus_stack"
			case "pass":
				report.Status = "pass"
			case "motion_nan":
				report.Reason = "PANORAMA_REVIEW;motion_fraction=NaN"
			case "reason":
				report.Reason = "PANORAMA_REVIEW;motion_fraction=0;UNRECOGNIZED"
			case "identity":
				report.Identity = &parent
			case "transform":
				report.Transform = &parent
			}
			raw, err := canonical.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseMultiImageReport(raw, binding, parent); err == nil {
				t.Fatal("unbound report accepted")
			}
		})
	}
}
func TestMultiRegistrationStrictSourceCanvasAndProjection(t *testing.T) {
	binding, _, _ := multiReadFixture(t, "optical.panorama")
	frames, _, _, _ := binding.Inputs()
	target := strings.Repeat("d", 64)
	for _, nonlinear := range []bool{false, true} {
		value := MultiRegistration{SchemaVersion: 1, Source: frames[0], ToCanvasHash: target, Homography: []float64{1, 0, 0, 0, 1, 0, 0, 0, 1}}
		if nonlinear {
			value.InverseField = &v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("e", 64)}
		}
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseMultiRegistration(raw, frames[0], target, nonlinear)
		if err != nil {
			t.Fatal(err)
		}
		parsed.Homography[0] = 2
		again, err := ParseMultiRegistration(raw, frames[0], target, nonlinear)
		if err != nil || again.Homography[0] != 1 {
			t.Fatal("matrix aliases input")
		}
		if _, err := ParseMultiRegistration(raw, frames[1], target, nonlinear); err == nil {
			t.Fatal("foreign source")
		}
		if _, err := ParseMultiRegistration(raw, frames[0], strings.Repeat("f", 64), nonlinear); err == nil {
			t.Fatal("foreign canvas")
		}
		if _, err := ParseMultiRegistration(raw, frames[0], target, !nonlinear); err == nil {
			t.Fatal("projection mismatch")
		}
		value.Homography[4] = 0
		raw, _ = canonical.Marshal(value)
		if _, err := ParseMultiRegistration(raw, frames[0], target, nonlinear); err == nil {
			t.Fatal("singular registration")
		}
	}
}
