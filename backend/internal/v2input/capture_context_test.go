package v2input

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestCaptureContextRequiresFrozenCoverageAndShotHash(t *testing.T) {
	id := uuid.New()
	shots := CaptureShotList{1, []CaptureShotRequirement{{id, "Portrait", []uuid.UUID{}, 1, .7}}}
	sha, err := canonical.Hash(shots)
	if err != nil {
		t.Fatal(err)
	}
	c := CaptureContext{1, uuid.New(), []uuid.UUID{}, Ref{uuid.New(), sha}, []string{}, Ref{uuid.New(), strings.Repeat("a", 64)}, []CaptureCoverage{{id, 0}}}
	if _, err := CompileCaptureContext(c, shots); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*CaptureContext)
	}{
		{"missing", func(v *CaptureContext) { v.Coverage = []CaptureCoverage{} }},
		{"foreign", func(v *CaptureContext) { v.Coverage = []CaptureCoverage{{uuid.New(), 1}} }},
		{"overflow", func(v *CaptureContext) { v.Coverage = []CaptureCoverage{{id, 5001}} }},
		{"wrong immutable body", func(v *CaptureContext) { v.ShotList.SHA256 = strings.Repeat("b", 64) }},
		{"null people", func(v *CaptureContext) { v.ConfirmedPersonIDs = nil }},
		{"invalid original sha", func(v *CaptureContext) { v.RecentContentSHAs = []string{"not a hash"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := c
			tc.change(&bad)
			if _, err := CompileCaptureContext(bad, shots); err == nil {
				t.Fatal("invalid context accepted")
			}
		})
	}
}
