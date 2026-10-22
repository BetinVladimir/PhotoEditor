package semanticproposal

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func TestSemanticPolicyExpectedPreservesNullAndTrustedMPSResources(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, backend := range []string{"cpu", "cuda", "mps"} {
		t.Run(backend, func(t *testing.T) {
			row := qualificationPolicyRow{ID: uuid.New(), Operation: "face.detect", Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: backend, Issuer: uuid.New(), Checks: []string{"b", "a"}, Fixtures: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true}
			if backend == "mps" {
				row.MPSResources = &v2qualification.MPSResourcePolicy{Metric: v2qualification.MPSAllocatorMetric, AllocatorBytes: 4096, P95MS: 2.5, RSSBytes: 8192, ScratchBytes: 1024}
			}
			body, err := canonical.Marshal([]qualificationPolicyRow{row})
			if err != nil {
				t.Fatal(err)
			}
			selected, err := selectQualificationPolicies(qualificationPolicySnapshot{body: string(body)}, []string{"face.detect"})
			if err != nil {
				t.Fatal(err)
			}
			got := selected["face.detect"]
			raw, err := canonical.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if json.Unmarshal(raw, &wire) != nil {
				t.Fatal("decode")
			}
			resources, exists := wire["MPSResources"]
			if !exists {
				t.Fatal("missing exact Expected field")
			}
			if backend != "mps" {
				if string(resources) != "null" || got.MPSResources != nil {
					t.Fatal("non-MPS gained resources")
				}
				return
			}
			expectedResources, _ := canonical.Marshal(row.MPSResources)
			if string(resources) != string(expectedResources) {
				t.Fatal("trusted resource projection differs")
			}
			// Expected owns its deployment policy; mutating the source must not change it.
			row.MPSResources.AllocatorBytes++
			if got.MPSResources.AllocatorBytes != 4096 {
				t.Fatal("resource pointer aliased")
			}
			for name, mutate := range map[string]func(*v2qualification.MPSResourcePolicy){
				"metric":    func(p *v2qualification.MPSResourcePolicy) { p.Metric = "foreign" },
				"allocator": func(p *v2qualification.MPSResourcePolicy) { p.AllocatorBytes++ },
				"latency":   func(p *v2qualification.MPSResourcePolicy) { p.P95MS++ },
				"RSS":       func(p *v2qualification.MPSResourcePolicy) { p.RSSBytes++ },
				"scratch":   func(p *v2qualification.MPSResourcePolicy) { p.ScratchBytes++ },
			} {
				t.Run(name, func(t *testing.T) {
					changed := ownQualificationExpected(got)
					mutate(changed.MPSResources)
					if sameQualificationPolicyExpected(got, changed) {
						t.Fatal("foreign resource bound matched")
					}
					if got.MPSResources.AllocatorBytes != 4096 || got.MPSResources.P95MS != 2.5 || got.MPSResources.RSSBytes != 8192 || got.MPSResources.ScratchBytes != 1024 || got.MPSResources.Metric != v2qualification.MPSAllocatorMetric {
						t.Fatal("copy mutated original")
					}
				})
			}
		})
	}
}

func TestSemanticPolicyExpectedRejectsUntrustedMPSResourceShapes(t *testing.T) {
	sha := strings.Repeat("a", 64)
	resource := v2qualification.MPSResourcePolicy{Metric: v2qualification.MPSAllocatorMetric, AllocatorBytes: 4096, P95MS: 2.5, RSSBytes: 8192, ScratchBytes: 1024}
	cases := []struct {
		name, backend, op string
		change            func(*v2qualification.MPSResourcePolicy)
	}{

		{name: "cpu", backend: "cpu", op: "face.detect"}, {name: "cuda", backend: "cuda", op: "face.detect"},
		{name: "unsupported defects operation", backend: "mps", op: "defects.propose"},
		{name: "unsupported semantic operation", backend: "mps", op: "semantic.transfer"},
		{name: "wrong metric", backend: "mps", op: "face.detect", change: func(p *v2qualification.MPSResourcePolicy) { p.Metric = "other" }},
		{name: "zero allocator", backend: "mps", op: "face.detect", change: func(p *v2qualification.MPSResourcePolicy) { p.AllocatorBytes = 0 }},
		{name: "fractional RSS", backend: "mps", op: "face.detect", change: func(p *v2qualification.MPSResourcePolicy) { p.RSSBytes = 1.5 }},
		{name: "zero scratch", backend: "mps", op: "face.detect", change: func(p *v2qualification.MPSResourcePolicy) { p.ScratchBytes = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := resource
			if tc.change != nil {
				tc.change(&p)
			}
			row := qualificationPolicyRow{ID: uuid.New(), Operation: tc.op, Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: tc.backend, Issuer: uuid.New(), Checks: []string{"a"}, Fixtures: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true, MPSResources: &p}
			body, err := canonical.Marshal([]qualificationPolicyRow{row})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = selectQualificationPolicies(qualificationPolicySnapshot{body: string(body)}, []string{tc.op}); err == nil {
				t.Fatal("invalid actual policy accepted")
			}
		})
	}
}

// Nil means common-report policy, including actual MPS semantic operations;
// only a nonnil resource policy selects the specialized MPS report contract.
func TestSemanticPolicyExpectedPreservesOptionalMPSCommonReportPolicy(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, op := range []string{"semantic.transfer", "defects.propose", "face.detect", "face.landmarks", "face.parse", "mask.person"} {
		t.Run(op, func(t *testing.T) {
			row := qualificationPolicyRow{ID: uuid.New(), Operation: op, Version: "1", Model: sha, Runtime: sha, Device: sha, Dataset: sha, Backend: "mps", Issuer: uuid.New(), Checks: []string{"a"}, Fixtures: []string{sha}, Evidence: []string{sha}, License: true, Enabled: true, Admission: true}
			body, err := canonical.Marshal([]qualificationPolicyRow{row})
			if err != nil {
				t.Fatal(err)
			}
			got, err := selectQualificationPolicies(qualificationPolicySnapshot{body: string(body)}, []string{op})
			if err != nil {
				t.Fatal(err)
			}
			if got[op].MPSResources != nil {
				t.Fatal("fabricated MPS resource policy")
			}
			raw, err := canonical.Marshal(got[op])
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if json.Unmarshal(raw, &wire) != nil || string(wire["MPSResources"]) != "null" {
				t.Fatal("optional resource representation changed")
			}
		})
	}
}
