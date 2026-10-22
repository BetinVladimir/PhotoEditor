package v2qualification

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
	"time"
)

func mixedFixture(t *testing.T) (MixedAuthority, MixedExpected, MixedReport) {
	t.Helper()
	pin := strings.Repeat("a", 64)
	gpu := MixedChild{Qualification: uuid.New(), Report: uuid.New(), ReportSHA: pin, Operation: "face.detect", Version: "1", Backend: "mps", Model: pin, Runtime: strings.Repeat("b", 64), Device: pin, Dataset: pin, PolicySHA: pin, Resources: &MixedEnvelope{RAM: 4096, VRAM: 20000000000, Scratch: 8192, P95MS: 50}}
	cpu := gpu
	cpu.Qualification = uuid.New()
	cpu.Report = uuid.New()
	cpu.Operation = "render.compose"
	cpu.Backend = "cpu"
	cpu.Resources = nil
	children := SortedChildren([]MixedChild{cpu, gpu})
	raw, e := canonical.Marshal(children)
	if e != nil {
		t.Fatal(e)
	}
	_, digest := mpsEncode(t, json.RawMessage(raw))
	a := MixedAuthority{Schema: 1, Scope: "renderer.staged.mixed", Tenant: uuid.New(), Project: uuid.New(), Parent: uuid.New(), Report: uuid.New(), ReportSHA: pin, DependenciesSHA: digest, Routes: map[string]string{"face.detect": "mps", "render.compose": "cpu", "quality.check": "cpu"}, Children: children}
	checks := []string{"rgb_golden", "alpha_golden", "generated_golden", "finite", "protected_unchanged", "outside_roi_unchanged", "stage_dependency_binding"}
	ex := MixedExpected{Common: Expected{Operation: "renderer.staged", Version: "1", Backend: "cpu", ModelLockSHA: pin, RuntimeLockSHA: pin, DeviceProfileSHA: pin, DatasetSHA: pin, IssuerID: uuid.New(), RequiredChecks: checks, TestFixtureSHAs: map[string]struct{}{pin: {}}, EvidenceSHAs: map[string]struct{}{pin: {}}, LicenseCleared: true}, Children: children, Routes: a.Routes, MaxP95: 100, MaxRSS: 4096, MaxScratch: 8192}
	report := MixedReport{Schema: 2, Scope: a.Scope, Operation: "renderer.staged", Version: "1", Model: pin, Runtime: pin, Device: pin, Dataset: pin, Fixtures: []string{pin}, Metrics: MPSMetrics{P50MS: 1, P95MS: 2, RSSBytes: 1024, ScratchBytes: 2048}, Status: "passed", Issuer: ex.Common.IssuerID, Created: json.RawMessage(`"` + time.Now().UTC().Format(time.RFC3339Nano) + `"`), Children: children, Routes: a.Routes, DependenciesSHA: digest}
	for _, id := range checks {
		report.Checks = append(report.Checks, Check{ID: id, Passed: true, EvidenceSHA: pin})
	}
	return a, ex, report
}
func TestMixedActualStageProfileUsesConservativeChildEnvelope(t *testing.T) {
	a, _, _ := mixedFixture(t)
	if e := a.Validate(a.Tenant, a.Project); e != nil {
		t.Fatal(e)
	}
	profile := []byte(`{"backend":"mps","model_lock_id":"` + strings.Repeat("a", 64) + `","precision":"float32","resource_estimate":{"ram_bytes":1,"vram_bytes":1073741824,"scratch_bytes":1,"output_bytes":9,"pixel_count":1}}`)
	actual, e := a.QualifyProfile("face.detect", "1", profile)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.ValidateProfile("face.detect", "1", actual); e != nil {
		t.Fatal(e)
	}
	var fields map[string]any
	_ = json.Unmarshal(actual, &fields)
	cost := fields["resource_estimate"].(map[string]any)
	if cost["vram_bytes"] != float64(20000000000) || cost["output_bytes"] != float64(9) {
		t.Fatal("wrong reservation", cost)
	}
	if a.ValidateProfile("face.detect", "1", profile) == nil {
		t.Fatal("underreserved profile accepted")
	}
	fields["mixed_qualification_pin"].(map[string]any)["report_id"] = uuid.NewString()
	forged, _ := canonical.Marshal(fields)
	if a.ValidateProfile("face.detect", "1", forged) == nil {
		t.Fatal("substituted child report accepted")
	}
	if _, e = a.QualifyProfile("render.compose", "1", profile); e == nil {
		t.Fatal("host compose relabelled mps")
	}
}
func TestMixedWholeParentCannotReplaceOperationChildProof(t *testing.T) {
	_, ex, report := mixedFixture(t)
	raw, pin := mpsEncode(t, report)
	if _, e := VerifyMixed(raw, pin, ex); e != nil {
		t.Fatal(e)
	}
	if _, e := Verify(raw, pin, ex.Common); e == nil {
		t.Fatal("parent became legacy kernel report")
	}
	for _, name := range []string{"issuer", "child_report", "child_scope", "child_runtime", "dataset", "module_check", "failed_check", "resource", "dependency_digest", "cpu_compose", "foreign_evidence", "fractional_rss", "unknown_field"} {
		t.Run(name, func(t *testing.T) {
			_, want, value := mixedFixture(t)
			body, _ := canonical.Marshal(value)
			var fields map[string]any
			_ = json.Unmarshal(body, &fields)
			switch name {
			case "issuer":
				fields["issuer_id"] = uuid.NewString()
			case "child_report":
				fields["children"].([]any)[0].(map[string]any)["report_id"] = uuid.NewString()
			case "child_scope":
				fields["scope"] = "render.compose"
			case "child_runtime":
				fields["children"].([]any)[0].(map[string]any)["runtime_lock_sha"] = strings.Repeat("0", 64)
			case "dataset":
				fields["dataset_sha"] = strings.Repeat("0", 64)
			case "module_check":
				want.Common.RequiredChecks = []string{"rgb_golden"}
			case "failed_check":
				fields["checks"].([]any)[0].(map[string]any)["passed"] = false
			case "resource":
				want.MaxP95 = 1
			case "dependency_digest":
				fields["dependencies_sha256"] = strings.Repeat("0", 64)
			case "cpu_compose":
				fields["stage_routes"].(map[string]any)["render.compose"] = "mps"
			case "foreign_evidence":
				fields["checks"].([]any)[0].(map[string]any)["evidence_sha"] = strings.Repeat("0", 64)
			case "fractional_rss":
				fields["metrics"].(map[string]any)["peak_rss_bytes"] = 1.5
			case "unknown_field":
				fields["backend"] = "mps"
			}
			raw, pin := mpsEncode(t, fields)
			if _, e := VerifyMixed(raw, pin, want); e == nil {
				t.Fatal("invalid parent accepted")
			}
		})
	}
}
func TestMixedDecodeRefusesTrailingData(t *testing.T) {
	a, _, _ := mixedFixture(t)
	raw, _ := canonical.Marshal(a)
	if _, e := DecodeMixedAuthority(append(raw, []byte(" garbage")...), a.Tenant, a.Project); e == nil {
		t.Fatal("trailing garbage accepted")
	}
}
