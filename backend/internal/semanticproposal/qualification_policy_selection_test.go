package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestRequiredQualificationPolicySelection(t *testing.T) {
	sha := strings.Repeat("a", 64)
	row := qualificationPolicyRow{ID: uuid.New(), Operation: "mask.person", Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: "cpu", Issuer: uuid.New(), Checks: []string{"z", "a"}, Fixtures: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true}
	snapshot := func(rows []qualificationPolicyRow) qualificationPolicySnapshot {
		body, _ := canonical.Marshal(rows)
		return qualificationPolicySnapshot{body: string(body)}
	}
	selected, err := selectQualificationPolicies(snapshot([]qualificationPolicyRow{row}), []string{"mask.person"})
	if err != nil || len(selected) != 1 || selected["mask.person"].RequiredChecks[0] != "a" {
		t.Fatal(selected, err)
	}
	chosen := selected["mask.person"]
	if !sameQualificationPolicyExpected(chosen, chosen) {
		t.Fatal("identical policy mismatch")
	}
	changed := ownQualificationExpected(chosen)
	changed.RequiredChecks = []string{"z", "a"}
	if !sameQualificationPolicyExpected(chosen, changed) {
		t.Fatal("check ordering changed policy")
	}
	for name, mutate := range map[string]func(*v2qualification.Expected){
		"runtime":          func(v *v2qualification.Expected) { v.RuntimeLockSHA = strings.Repeat("b", 64) },
		"issuer":           func(v *v2qualification.Expected) { v.IssuerID = uuid.New() },
		"fixture":          func(v *v2qualification.Expected) { v.TestFixtureSHAs[strings.Repeat("b", 64)] = struct{}{} },
		"evidence":         func(v *v2qualification.Expected) { clear(v.EvidenceSHAs) },
		"checks":           func(v *v2qualification.Expected) { v.RequiredChecks = append(v.RequiredChecks, "extra") },
		"duplicate checks": func(v *v2qualification.Expected) { v.RequiredChecks = append(v.RequiredChecks, v.RequiredChecks[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := ownQualificationExpected(chosen)
			mutate(&changed)
			if sameQualificationPolicyExpected(chosen, changed) {
				t.Fatal("foreign expected policy matched")
			}
		})
	}
	for _, required := range [][]string{{}, {"mask.person", "mask.person"}, {"unknown"}, {"face.parse"}} {
		if got, err := selectQualificationPolicies(snapshot([]qualificationPolicyRow{row}), required); err == nil || got != nil {
			t.Fatal("missing/invalid required policy", required)
		}
	}
	for name, mutate := range map[string]func(*qualificationPolicyRow){"disabled": func(r *qualificationPolicyRow) { r.Enabled = false }, "not admitted": func(r *qualificationPolicyRow) { r.Admission = false }, "license": func(r *qualificationPolicyRow) { r.License = false }, "digest": func(r *qualificationPolicyRow) { r.Runtime = "bad" }, "duplicate checks": func(r *qualificationPolicyRow) { r.Checks = []string{"a", "a"} }, "duplicate fixtures": func(r *qualificationPolicyRow) { r.Fixtures = []string{sha, sha} }} {
		t.Run(name, func(t *testing.T) {
			changed := row
			mutate(&changed)
			if got, err := selectQualificationPolicies(snapshot([]qualificationPolicyRow{changed}), []string{"mask.person"}); err == nil || got != nil {
				t.Fatal("invalid policy selected")
			}
		})
	}
	duplicate := row
	duplicate.ID = uuid.New()
	if got, err := selectQualificationPolicies(snapshot([]qualificationPolicyRow{row, duplicate}), []string{"mask.person"}); err == nil || got != nil {
		t.Fatal("ambiguous policy selected")
	}
	defectRow := row
	defectRow.ID = uuid.New()
	defectRow.Operation = "defects.propose"
	transferRow := row
	transferRow.ID = uuid.New()
	transferRow.Operation = "semantic.transfer"
	executionSnapshot := snapshot([]qualificationPolicyRow{defectRow, transferRow})
	executionPolicies, err := selectQualificationPolicies(executionSnapshot, []string{"defects.propose", "semantic.transfer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := matchProposalExecutionPolicies(executionSnapshot, executionPolicies["defects.propose"], executionPolicies["semantic.transfer"]); err != nil {
		t.Fatal(err)
	}
	if err := matchProposalExecutionPolicies(snapshot([]qualificationPolicyRow{transferRow}), executionPolicies["defects.propose"], executionPolicies["semantic.transfer"]); err == nil {
		t.Fatal("missing defect policy accepted")
	}
	foreignExecution := ownQualificationExpected(executionPolicies["semantic.transfer"])
	foreignExecution.DeviceProfileSHA = strings.Repeat("b", 64)
	if err := matchProposalExecutionPolicies(executionSnapshot, executionPolicies["defects.propose"], foreignExecution); err == nil {
		t.Fatal("foreign execution profile accepted")
	}

}
