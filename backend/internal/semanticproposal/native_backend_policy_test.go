package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestNativeMetalPolicyAxesRemainIndependentlyQualified(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ops := []string{"mask.person", "face.detect", "face.landmarks", "face.parse", "defects.propose", "semantic.transfer"}
	rows := make([]qualificationPolicyRow, 0, len(ops))
	for _, op := range ops {
		backend := "mps"
		if op == "defects.propose" {
			backend = "cpu"
		}
		rows = append(rows, qualificationPolicyRow{ID: uuid.New(), Operation: op, Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: backend, Issuer: uuid.New(), Checks: []string{"check"}, Fixtures: []string{sha}, Calibrations: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true})
	}
	snapshot := func(r []qualificationPolicyRow) qualificationPolicySnapshot {
		raw, e := canonical.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		return qualificationPolicySnapshot{body: string(raw)}
	}
	selected, e := selectNativeQualificationPolicies(snapshot(rows), []string{"face.detect"})
	if e != nil || selected["face.detect"].Backend != "mps" {
		t.Fatal(e)
	}
	for i := range rows {
		changed := append([]qualificationPolicyRow(nil), rows...)
		changed[i].Backend = "cuda"
		if got, e := selectNativeQualificationPolicies(snapshot(changed), []string{"face.parse"}); e == nil || got != nil {
			t.Fatal("mixed native producer policy admitted", rows[i].Operation)
		}
	}
	for i := range rows {
		changed := append([]qualificationPolicyRow(nil), rows[:i]...)
		changed = append(changed, rows[i+1:]...)
		if got, e := selectNativeQualificationPolicies(snapshot(changed), []string{"face.parse"}); e == nil || got != nil {
			t.Fatal("missing independent model policy admitted", rows[i].Operation)
		}
	}
	// Existing independent CUDA client still needs only its original selection.
	cuda := rows[0]
	cuda.Backend = "cuda"
	if got, e := selectNativeQualificationPolicies(snapshot([]qualificationPolicyRow{cuda}), []string{"mask.person"}); e != nil || got["mask.person"].Backend != "cuda" {
		t.Fatal("legacy CUDA scope expanded", e)
	}
}
